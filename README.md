# T4 Playground

Browser playground bindings for [T4](https://github.com/t4db/t4), compiled to WebAssembly.

The public playground is available at:

https://t4db.github.io/t4/playground/

## Build

Requirements:

- Go 1.25.1 or newer
- A Unix-like shell

Build the release bundle:

```sh
./scripts/build.sh
```

The build writes `dist/t4play.wasm`.

To build only the Go WebAssembly module:

```sh
GOOS=js GOARCH=wasm go build -o dist/t4play.wasm ./cmd/playground
```

