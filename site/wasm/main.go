//go:build js && wasm

// Command wasm exposes the sweetspot terminal UI to the website. The radio data is simulated.
package main

import (
	"encoding/json"
	"syscall/js"
	"unsafe"

	"github.com/bvolpato/sweetspot/internal/ui"
)

func main() {
	demos := map[int]*ui.Demo{}
	export := func(f func(d *ui.Demo, args []js.Value) any) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			d := demos[args[0].Int()]
			if d == nil {
				return nil
			}
			return f(d, args[1:])
		})
	}
	js.Global().Set("sweetspot", map[string]any{
		"create": js.FuncOf(func(_ js.Value, a []js.Value) any {
			demos[a[0].Int()] = ui.NewDemo(a[1].Int(), a[2].Int())
			return nil
		}),
		"resize": export(func(d *ui.Demo, a []js.Value) any { d.Resize(a[0].Int(), a[1].Int()); return nil }),
		"key":    export(func(d *ui.Demo, a []js.Value) any { d.Key(a[0].String()); return nil }),
		"pin":    export(func(d *ui.Demo, a []js.Value) any { d.Pin(a[0].String()); return nil }),
		"state":  export(func(d *ui.Demo, _ []js.Value) any { return d.State() }),
		"step": export(func(d *ui.Demo, a []js.Value) any {
			var samples []ui.DemoSample
			if err := json.Unmarshal([]byte(a[0].String()), &samples); err != nil {
				return err.Error()
			}
			for _, s := range samples {
				d.Step(s)
			}
			return nil
		}),
		"cells": export(func(d *ui.Demo, _ []js.Value) any {
			cells := d.Cells()
			out := js.Global().Get("Uint8Array").New(len(cells) * 4)
			if len(cells) > 0 {
				js.CopyBytesToJS(out, unsafe.Slice((*byte)(unsafe.Pointer(&cells[0])), len(cells)*4))
			}
			return out
		}),
	})
	if ready := js.Global().Get("sweetspotReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
