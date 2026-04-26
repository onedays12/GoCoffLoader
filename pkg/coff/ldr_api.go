package coff

import (
	"encoding/binary"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

func currentPEB() uintptr

type listEntry struct {
	Flink uintptr
	Blink uintptr
}

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        uintptr
}

type peb struct {
	Reserved [0x18]byte
	Ldr      *pebLdrData
}

type pebLdrData struct {
	Length                  uint32
	Initialized             byte
	Reserved1               [3]byte
	SsHandle                uintptr
	InLoadOrderModuleList   listEntry
	InMemoryOrderModuleList listEntry
	InInitOrderModuleList   listEntry
}

type ldrDataTableEntry struct {
	InLoadOrderLinks           listEntry
	InMemoryOrderLinks         listEntry
	InInitializationOrderLinks listEntry
	DllBase                    uintptr
	EntryPoint                 uintptr
	SizeOfImage                uint32
	FullDllName                unicodeString
	BaseDllName                unicodeString
}

type ldrAPITable struct {
	CloseHandle                       uintptr
	GetProcessHeap                    uintptr
	HeapAlloc                         uintptr
	HeapFree                          uintptr
	MultiByteToWideChar               uintptr
	RtlAddVectoredExceptionHandler    uintptr
	RtlExitUserThread                 uintptr
	RtlRemoveVectoredExceptionHandler uintptr
	VirtualAlloc                      uintptr
	VirtualFree                       uintptr
	VirtualProtect                    uintptr
	LdrLoadDll                        uintptr
	NtCreateThreadEx                  uintptr
	NtGetContextThread                uintptr
	NtResumeThread                    uintptr
	NtSetContextThread                uintptr
	NtWaitForSingleObject             uintptr
}

var (
	ldrLoadDLLAddr uintptr
	ldrAPI         = mustInitLdrAPI()
)

func mustInitLdrAPI() ldrAPITable {
	kernel32 := getModuleByPEBName("kernel32.dll")
	ntdll := getModuleByPEBName("ntdll.dll")
	if kernel32 == 0 || ntdll == 0 {
		panic("failed to locate kernel32.dll or ntdll.dll from PEB")
	}

	ldrLoadDLLAddr = mustExport(ntdll, "LdrLoadDll")
	api := ldrAPITable{
		CloseHandle:                       mustExport(kernel32, "CloseHandle"),
		GetProcessHeap:                    mustExport(kernel32, "GetProcessHeap"),
		HeapAlloc:                         mustExport(kernel32, "HeapAlloc"),
		HeapFree:                          mustExport(kernel32, "HeapFree"),
		MultiByteToWideChar:               mustExport(kernel32, "MultiByteToWideChar"),
		VirtualAlloc:                      mustExport(kernel32, "VirtualAlloc"),
		VirtualFree:                       mustExport(kernel32, "VirtualFree"),
		VirtualProtect:                    mustExport(kernel32, "VirtualProtect"),
		LdrLoadDll:                        ldrLoadDLLAddr,
		NtCreateThreadEx:                  mustExport(ntdll, "NtCreateThreadEx"),
		NtGetContextThread:                mustExport(ntdll, "NtGetContextThread"),
		NtResumeThread:                    mustExport(ntdll, "NtResumeThread"),
		NtSetContextThread:                mustExport(ntdll, "NtSetContextThread"),
		NtWaitForSingleObject:             mustExport(ntdll, "NtWaitForSingleObject"),
		RtlAddVectoredExceptionHandler:    mustExport(ntdll, "RtlAddVectoredExceptionHandler"),
		RtlExitUserThread:                 mustExport(ntdll, "RtlExitUserThread"),
		RtlRemoveVectoredExceptionHandler: mustExport(ntdll, "RtlRemoveVectoredExceptionHandler"),
	}
	return api
}

func mustExport(module uintptr, name string) uintptr {
	addr := resolveExportByName(module, name)
	if addr == 0 {
		panic("failed to resolve export: " + name)
	}
	return addr
}

func getModuleByPEBName(name string) uintptr {
	pebAddr := currentPEB()
	if pebAddr == 0 {
		return 0
	}

	p := (*peb)(unsafe.Pointer(pebAddr))
	if p.Ldr == nil {
		return 0
	}

	head := uintptr(unsafe.Pointer(&p.Ldr.InMemoryOrderModuleList))
	current := p.Ldr.InMemoryOrderModuleList.Flink
	offset := unsafe.Offsetof(ldrDataTableEntry{}.InMemoryOrderLinks)

	for current != 0 && current != head {
		entry := (*ldrDataTableEntry)(unsafe.Pointer(current - offset))
		if entry.DllBase != 0 {
			baseName := readUTF16String(entry.BaseDllName.Buffer, int(entry.BaseDllName.Length/2))
			if strings.EqualFold(baseName, name) {
				return entry.DllBase
			}
		}
		current = (*listEntry)(unsafe.Pointer(current)).Flink
	}
	return 0
}

func resolveExportByName(module uintptr, name string) uintptr {
	addr, _, _ := resolveExportByNameDepth(module, name, 0)
	return addr
}

func resolveExportByNameDepth(module uintptr, name string, depth int) (uintptr, uint32, uint32) {
	if module == 0 || depth > 8 {
		return 0, 0, 0
	}

	exportsRVA, exportsSize := exportDirectory(module)
	if exportsRVA == 0 {
		return 0, 0, 0
	}

	exportDir := module + uintptr(exportsRVA)
	numberOfNames := readU32(exportDir + 0x18)
	addressOfFunctions := module + uintptr(readU32(exportDir+0x1c))
	addressOfNames := module + uintptr(readU32(exportDir+0x20))
	addressOfNameOrdinals := module + uintptr(readU32(exportDir+0x24))

	for i := uint32(0); i < numberOfNames; i++ {
		funcName := readCString(module + uintptr(readU32(addressOfNames+uintptr(i*4))))
		if funcName != name {
			continue
		}

		ordinal := readU16(addressOfNameOrdinals + uintptr(i*2))
		funcRVA := readU32(addressOfFunctions + uintptr(ordinal)*4)
		funcAddr := module + uintptr(funcRVA)
		if funcRVA >= exportsRVA && funcRVA < exportsRVA+exportsSize {
			return resolveForwardedExport(readCString(funcAddr), depth+1)
		}
		return funcAddr, exportsRVA, exportsSize
	}

	return 0, exportsRVA, exportsSize
}

func resolveForwardedExport(forwarder string, depth int) (uintptr, uint32, uint32) {
	dot := strings.LastIndexByte(forwarder, '.')
	if dot <= 0 || dot == len(forwarder)-1 {
		return 0, 0, 0
	}

	moduleName := forwarder[:dot]
	procName := forwarder[dot+1:]
	if !strings.Contains(moduleName, ".") {
		moduleName += ".dll"
	}

	module := getOrLoadModule(moduleName)
	if module == 0 || strings.HasPrefix(procName, "#") {
		return 0, 0, 0
	}
	return resolveExportByNameDepth(module, procName, depth)
}

func getOrLoadModule(name string) uintptr {
	if module := getModuleByPEBName(name); module != 0 {
		return module
	}

	module, _ := ldrLoadDLL(name)
	return module
}

func ldrLoadDLL(name string) (uintptr, uint32) {
	if ldrLoadDLLAddr == 0 {
		return 0, 0xC0000135
	}

	wide := utf16.Encode([]rune(name + "\x00"))
	us := unicodeString{
		Length:        uint16((len(wide) - 1) * 2),
		MaximumLength: uint16(len(wide) * 2),
		Buffer:        uintptr(unsafe.Pointer(&wide[0])),
	}

	var module uintptr
	status, _, _ := syscall.SyscallN(ldrLoadDLLAddr, 0, 0, uintptr(unsafe.Pointer(&us)), uintptr(unsafe.Pointer(&module)))
	return module, uint32(status)
}

func exportDirectory(module uintptr) (uint32, uint32) {
	if readU16(module) != 0x5A4D {
		return 0, 0
	}

	nt := module + uintptr(readU32(module+0x3c))
	if readU32(nt) != 0x00004550 {
		return 0, 0
	}

	optional := nt + 0x18
	magic := readU16(optional)
	dataDirectory := uintptr(0)
	switch magic {
	case 0x20b:
		dataDirectory = optional + 0x70
	case 0x10b:
		dataDirectory = optional + 0x60
	default:
		return 0, 0
	}

	return readU32(dataDirectory), readU32(dataDirectory + 4)
}

func readUTF16String(ptr uintptr, length int) string {
	if ptr == 0 || length <= 0 {
		return ""
	}

	buf := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), length)
	return string(utf16.Decode(buf))
}

func readU16(ptr uintptr) uint16 {
	return binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 2))
}

func readU32(ptr uintptr) uint32 {
	return binary.LittleEndian.Uint32(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), 4))
}

func resolveAPI(moduleName string, procName string) uintptr {
	module := getOrLoadModule(moduleName)
	if module == 0 {
		return 0
	}
	return resolveExportByName(module, procName)
}

func resolveAPIFromCommonModules(procName string) uintptr {
	for _, module := range []string{"kernel32.dll", "kernelbase.dll", "user32.dll", "advapi32.dll", "msvcrt.dll"} {
		if addr := resolveAPI(module, procName); addr != 0 {
			return addr
		}
	}
	return 0
}
