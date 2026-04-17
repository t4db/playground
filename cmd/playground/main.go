//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"syscall/js"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/vfs"
	"github.com/t4db/t4"
	iwal "github.com/t4db/t4/internal/wal"
)

var (
	node      *t4.Node
	watchStop context.CancelFunc
	flushStop context.CancelFunc
	watchLog  []map[string]any
	memFS     vfs.FS
	demoLog   *demoWAL
)

func main() {
	must(resetNode())

	api := map[string]any{
		"put":      js.FuncOf(put),
		"get":      js.FuncOf(get),
		"list":     js.FuncOf(list),
		"delete":   js.FuncOf(deleteKey),
		"watch":    js.FuncOf(watch),
		"events":   js.FuncOf(events),
		"flush":    js.FuncOf(flush),
		"fs":       js.FuncOf(fs),
		"wal":      js.FuncOf(wal),
		"revision": js.FuncOf(revision),
		"reset":    js.FuncOf(reset),
	}
	js.Global().Set("t4play", js.ValueOf(api))
	select {}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func resetNode() error {
	if watchStop != nil {
		watchStop()
		watchStop = nil
	}
	if flushStop != nil {
		flushStop()
		flushStop = nil
	}
	if node != nil {
		if err := node.Close(); err != nil {
			return err
		}
		node = nil
	}
	resetDemoStorage()
	watchLog = nil
	demoLog = &demoWAL{}
	var err error
	node, err = t4.Open(t4.Config{
		DataDir: "/t4play",
		Logger:  t4.NoopLogger,
		PebbleOptions: []func(*pebble.Options){
			func(o *pebble.Options) {
				o.FS = memFS
			},
		},
		WAL: demoLog,
	})
	if err == nil {
		ctx, cancel := context.WithCancel(context.Background())
		flushStop = cancel
		go flushLoop(ctx)
	}
	return err
}

func flushLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if node != nil {
				_ = node.Flush()
			}
		case <-ctx.Done():
			return
		}
	}
}

func put(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return jsError(errors.New("put requires key and value"))
	}
	rev, err := node.Put(context.Background(), args[0].String(), []byte(args[1].String()), 0)
	if err != nil {
		return jsError(err)
	}
	return js.ValueOf(map[string]any{"ok": true, "revision": rev})
}

func get(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError(errors.New("get requires key"))
	}
	kv, err := node.Get(args[0].String())
	if err != nil {
		return jsError(err)
	}
	if kv == nil {
		return js.ValueOf(map[string]any{"ok": true, "found": false})
	}
	return js.ValueOf(map[string]any{"ok": true, "found": true, "kv": kvToJS(kv)})
}

func list(this js.Value, args []js.Value) any {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0].String()
	}
	kvs, err := node.List(prefix)
	if err != nil {
		return jsError(err)
	}
	out := make([]any, len(kvs))
	for i, kv := range kvs {
		out[i] = kvToJS(kv)
	}
	return js.ValueOf(map[string]any{"ok": true, "items": out})
}

func deleteKey(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError(errors.New("delete requires key"))
	}
	rev, err := node.Delete(context.Background(), args[0].String())
	if err != nil {
		return jsError(err)
	}
	return js.ValueOf(map[string]any{"ok": true, "revision": rev})
}

func watch(this js.Value, args []js.Value) any {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0].String()
	}
	if watchStop != nil {
		watchStop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := node.Watch(ctx, prefix, 0)
	if err != nil {
		cancel()
		return jsError(err)
	}
	watchStop = cancel
	watchLog = nil
	go func() {
		for ev := range ch {
			watchLog = append(watchLog, eventToJS(ev))
		}
	}()
	return js.ValueOf(map[string]any{"ok": true, "prefix": prefix})
}

func events(this js.Value, args []js.Value) any {
	out := make([]any, len(watchLog))
	for i := range watchLog {
		out[i] = watchLog[i]
	}
	return js.ValueOf(map[string]any{"ok": true, "events": out})
}

func flush(this js.Value, args []js.Value) any {
	if err := node.Flush(); err != nil {
		return jsError(err)
	}
	return js.ValueOf(map[string]any{"ok": true, "revision": node.CurrentRevision()})
}

func fs(this js.Value, args []js.Value) any {
	files := demoFS()
	out := make([]any, len(files))
	for i, file := range files {
		out[i] = map[string]any{"path": file.Path, "isDir": file.IsDir, "size": file.Size}
	}
	return js.ValueOf(map[string]any{"ok": true, "files": out})
}

func wal(this js.Value, args []js.Value) any {
	entries := demoWALEntries()
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = map[string]any{
			"revision": e.Revision,
			"term":     fmt.Sprint(e.Term),
			"op":       e.Op,
			"key":      e.Key,
			"value":    string(e.Value),
		}
	}
	return js.ValueOf(map[string]any{"ok": true, "entries": out})
}

func revision(this js.Value, args []js.Value) any {
	return js.ValueOf(map[string]any{"ok": true, "revision": node.CurrentRevision()})
}

func reset(this js.Value, args []js.Value) any {
	if err := resetNode(); err != nil {
		return jsError(err)
	}
	return js.ValueOf(map[string]any{"ok": true})
}

func kvToJS(kv *t4.KeyValue) map[string]any {
	return map[string]any{
		"key":            kv.Key,
		"value":          string(kv.Value),
		"revision":       kv.Revision,
		"createRevision": kv.CreateRevision,
		"prevRevision":   kv.PrevRevision,
		"lease":          kv.Lease,
	}
}

func eventToJS(ev t4.Event) map[string]any {
	typ := "put"
	if ev.Type == t4.EventDelete {
		typ = "delete"
	}
	out := map[string]any{"type": typ, "kv": kvToJS(ev.KV)}
	if ev.PrevKV != nil {
		out["prevKv"] = kvToJS(ev.PrevKV)
	}
	return out
}

func jsError(err error) js.Value {
	return js.ValueOf(map[string]any{"ok": false, "error": err.Error()})
}

type demoFSFile struct {
	Path  string
	IsDir bool
	Size  int64
}

type demoWALEntry struct {
	Revision int64
	Term     uint64
	Op       string
	Key      string
	Value    []byte
}

func resetDemoStorage() {
	memFS = vfs.NewMem()
	demoLog = nil
}

func demoFS() []demoFSFile {
	var out []demoFSFile
	walkDemoFS("/", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkDemoFS(dir string, out *[]demoFSFile) {
	names, err := memFS.List(dir)
	if err != nil {
		return
	}
	for _, name := range names {
		path := memFS.PathJoin(dir, name)
		info, err := memFS.Stat(path)
		if err != nil {
			continue
		}
		*out = append(*out, demoFSFile{Path: path, IsDir: info.IsDir(), Size: info.Size()})
		if info.IsDir() {
			walkDemoFS(path, out)
		}
	}
}

func demoWALEntries() []demoWALEntry {
	if demoLog == nil {
		return nil
	}
	var out []demoWALEntry
	demoLog.mu.Lock()
	defer demoLog.mu.Unlock()
	for _, e := range demoLog.entries {
		out = append(out, demoWALEntry{
			Revision: e.Revision,
			Term:     e.Term,
			Op:       opName(e.Op),
			Key:      e.Key,
			Value:    append([]byte(nil), e.Value...),
		})
	}
	return out
}

type demoWAL struct {
	mu      sync.Mutex
	entries []iwal.Entry
	closed  bool
}

func (w *demoWAL) Open(dir string, term uint64, startRev int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = nil
	w.closed = false
	return nil
}

func (w *demoWAL) ReplayLocal(db iwal.RecoveryStore, afterRev int64) error {
	return nil
}

func (w *demoWAL) Append(e *iwal.Entry) error {
	return w.AppendBatch(context.Background(), []*iwal.Entry{e})
}

func (w *demoWAL) AppendBatch(ctx context.Context, entries []*iwal.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return t4.ErrClosed
	}
	for _, e := range entries {
		w.entries = append(w.entries, *e)
	}
	return nil
}

func (w *demoWAL) SealAndFlush(nextRev int64) error {
	return nil
}

func (w *demoWAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func opName(op iwal.Op) string {
	switch op {
	case iwal.OpCreate:
		return "create"
	case iwal.OpUpdate:
		return "update"
	case iwal.OpDelete:
		return "delete"
	case iwal.OpCompact:
		return "compact"
	case iwal.OpTxn:
		return "txn"
	default:
		return "unknown"
	}
}
