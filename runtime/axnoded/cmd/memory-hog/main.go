// memory-hog is the static, network-independent memcg OOM fixture used by the
// runtime conformance probe. It retains and touches every anonymous page so
// compiler or kernel optimizations cannot turn the workload into a no-op.
package main

import "runtime/debug"

func main() {
	debug.SetGCPercent(-1)
	const chunkBytes = 1 << 20
	retained := make([][]byte, 0, 1024)
	for {
		chunk := make([]byte, chunkBytes)
		for offset := 0; offset < len(chunk); offset += 4096 {
			chunk[offset] = byte(len(retained))
		}
		retained = append(retained, chunk)
	}
}
