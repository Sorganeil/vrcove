// Runs the spike in a Web Worker, where the real WASM client will live.
importScripts('wasm_exec.js');
const go = new Go();
go.env = { VRCOVE_RELAY: `ws://${self.location.host}/relay` };
const started = performance.now();
WebAssembly.instantiateStreaming(fetch('vrcove-wasm.wasm'), go.importObject)
  .then((result) => {
    postMessage(`wasm loaded and compiled in ${Math.round(performance.now() - started)} ms`);
    return go.run(result.instance);
  })
  .catch((err) => postMessage(`FAIL: ${err}`));
