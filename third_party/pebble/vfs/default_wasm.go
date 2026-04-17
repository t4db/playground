// Copyright 2023 The LevelDB-Go and Pebble Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

//go:build wasm
// +build wasm

package vfs

import (
	"os"

	"github.com/cockroachdb/errors"
)

func wrapOSFileImpl(f *os.File) File {
	return &wasmFile{File: f}
}

func (defaultFS) OpenDir(name string) (File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &wasmFile{File: f}, nil
}

var _ File = (*wasmFile)(nil)

type wasmFile struct {
	*os.File
}

func (*wasmFile) Prefetch(offset int64, length int64) error { return nil }
func (*wasmFile) Preallocate(offset, length int64) error    { return nil }

func (f *wasmFile) SyncData() error {
	return f.Sync()
}

func (f *wasmFile) SyncTo(int64) (fullSync bool, err error) {
	if err = f.Sync(); err != nil {
		return false, err
	}
	return true, nil
}
