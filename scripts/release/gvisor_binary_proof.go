// Verify compiler-generated executable and inline function metadata for a selected gVisor binary.
package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type proof struct {
	GoVersion       string `json:"go_version"`
	Functions       int    `json:"functions"`
	Names           int    `json:"names"`
	HTTPBodyLimit   bool   `json:"has_http_body_limit"`
	CiliumBTFParser bool   `json:"has_cilium_btf_parser"`
}

func inspect(name string, machine elf.Machine) (proof, error) {
	var result proof
	file, err := elf.Open(name)
	if err != nil {
		return result, err
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.ByteOrder != binary.LittleEndian || file.Machine != machine {
		return result, errors.New("unexpected Linux ELF architecture")
	}
	info, err := buildinfo.ReadFile(name)
	if err != nil {
		return result, err
	}
	result.GoVersion = info.GoVersion
	text, pcln := file.Section(".text"), file.Section(".gopclntab")
	if text == nil || pcln == nil || pcln.Size > 64<<20 {
		return result, errors.New("missing or oversized compiler function metadata")
	}
	data, err := pcln.Data()
	if err != nil {
		return result, err
	}
	// The Go linker puts direct and inlined names in funcnametab and follows it with cutab.
	// Cross-check the declared function count and every decoded function against that table.
	if len(data) < 72 || data[4] != 0 || data[5] != 0 || data[6] == 0 || data[7] != 8 {
		return result, errors.New("invalid Go function metadata header")
	}
	functionCount := binary.LittleEndian.Uint64(data[8:16])
	start, end := binary.LittleEndian.Uint64(data[32:40]), binary.LittleEndian.Uint64(data[40:48])
	if start < 72 || start >= end || end > uint64(len(data)) || data[end-1] != 0 {
		return result, errors.New("invalid compiler function name table")
	}
	table, err := gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
	if err != nil || functionCount == 0 || uint64(len(table.Funcs)) != functionCount {
		return result, errors.New("incomplete Go executable function table")
	}
	names := make(map[string]bool)
	for _, raw := range bytes.Split(data[start:end], []byte{0}) {
		if len(raw) != 0 {
			names[string(raw)] = true
		}
	}
	for _, f := range table.Funcs {
		if f.Sym == nil || !names[f.Name] || f.Entry < text.Addr || f.Entry-text.Addr >= text.Size {
			return result, errors.New("Go executable function is missing from the decoded metadata")
		}
	}
	if !names["runtime.main"] || !names["main.main"] {
		return result, errors.New("Go executable entry point is missing from the decoded metadata")
	}
	result.Functions, result.Names = len(table.Funcs), len(names)
	for name := range names {
		result.HTTPBodyLimit = result.HTTPBodyLimit || strings.Contains(name, "net/http.(*maxBytesReader).Read") ||
			strings.Contains(name, "net/http.(*response).requestTooLarge") || strings.Contains(name, "net/http.MaxBytesReader")
		result.CiliumBTFParser = result.CiliumBTFParser || strings.Contains(name, "github.com/cilium/ebpf/btf.")
	}
	return result, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gvisor-binary-proof amd64|arm64 ELF_FILE")
		os.Exit(1)
	}
	machine := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[os.Args[1]]
	result, err := inspect(os.Args[2], machine)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
