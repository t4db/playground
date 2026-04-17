// Copyright 2020 The LevelDB-Go and Pebble Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

//go:build wasm
// +build wasm

package vfs

func (defaultFS) GetDiskUsage(path string) (DiskUsage, error) {
	return DiskUsage{}, nil
}
