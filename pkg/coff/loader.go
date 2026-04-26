package coff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime/debug"
	"strings"
	"syscall"
	"unsafe"

	"github.com/RIscRIpt/pecoff"
	"github.com/RIscRIpt/pecoff/binutil"
	"github.com/RIscRIpt/pecoff/windef"
)

// --- Win32 常量 ---
const (
	memCommit             = 0x1000
	memReserve            = 0x2000
	memRelease            = 0x8000
	memTopDown            = 0x100000
	pageExecuteReadWrite  = 0x40
	pageReadWrite         = 0x04
	imageScnMemExecute    = 0x20000000
	contextAMD64          = 0x100000
	contextAll            = contextAMD64 | 0x1 | 0x2 | 0x4 | 0x8 | 0x10
	exceptionContinue     = 0xFFFFFFFF
	threadAllAccess       = 0x001F0FFF
	threadCreateSuspended = 0x00000001
)

var vehCallback = syscall.NewCallback(vectoredExceptionHandler)

// SectionMap 存储映射后的节区信息
type SectionMap struct {
	Ptr  uintptr
	Size uint32
}

// Coffee 加载器核心结构体
type Coffee struct {
	Data      []byte
	Symbols   []*pecoff.Symbol
	Sections  []*pecoff.Section
	SecMap    []SectionMap
	ImageBase uintptr
	TotalSize uintptr
	GOT       uintptr
	BSS       uintptr
	GOTSize   uint32
	BSSSize   uint32
}

type m128a struct {
	Low  uint64
	High int64
}

type threadContext struct {
	P1Home uint64
	P2Home uint64
	P3Home uint64
	P4Home uint64
	P5Home uint64
	P6Home uint64

	ContextFlags uint32
	MxCsr        uint32

	SegCs  uint16
	SegDs  uint16
	SegEs  uint16
	SegFs  uint16
	SegGs  uint16
	SegSs  uint16
	EFlags uint32

	Dr0 uint64
	Dr1 uint64
	Dr2 uint64
	Dr3 uint64
	Dr6 uint64
	Dr7 uint64

	Rax uint64
	Rcx uint64
	Rdx uint64
	Rbx uint64
	Rsp uint64
	Rbp uint64
	Rsi uint64
	Rdi uint64
	R8  uint64
	R9  uint64
	R10 uint64
	R11 uint64
	R12 uint64
	R13 uint64
	R14 uint64
	R15 uint64
	Rip uint64

	Header               [2]m128a
	Legacy               [8]m128a
	Xmm0                 m128a
	Xmm1                 m128a
	Xmm2                 m128a
	Xmm3                 m128a
	Xmm4                 m128a
	Xmm5                 m128a
	Xmm6                 m128a
	Xmm7                 m128a
	Xmm8                 m128a
	Xmm9                 m128a
	Xmm10                m128a
	Xmm11                m128a
	Xmm12                m128a
	Xmm13                m128a
	Xmm14                m128a
	Xmm15                m128a
	VectorRegister       [26]m128a
	VectorControl        uint64
	DebugControl         uint64
	LastBranchToRip      uint64
	LastBranchFromRip    uint64
	LastExceptionToRip   uint64
	LastExceptionFromRip uint64
}

type exceptionPointers struct {
	ExceptionRecord uintptr
	ContextRecord   *threadContext
}

// alignUp 内存对齐
func alignUp(val uintptr) uintptr {
	return (val + 0xFFF) &^ 0xFFF
}

func winVirtualAlloc(address uintptr, size uintptr, allocationType uint32, protect uint32) (uintptr, error) {
	ret, _, err := syscall.SyscallN(ldrAPI.VirtualAlloc, address, size, uintptr(allocationType), uintptr(protect))
	if ret == 0 {
		return 0, err
	}
	return ret, nil
}

func winVirtualFree(address uintptr) error {
	if address == 0 {
		return nil
	}
	ret, _, err := syscall.SyscallN(ldrAPI.VirtualFree, address, 0, uintptr(memRelease))
	if ret == 0 {
		return err
	}
	return nil
}

func vectoredExceptionHandler(exceptionInfo uintptr) uintptr {
	if exceptionInfo == 0 {
		return 0
	}
	info := (*exceptionPointers)(unsafe.Pointer(exceptionInfo))
	if info.ContextRecord == nil {
		return 0
	}
	info.ContextRecord.Rip = uint64(ldrAPI.RtlExitUserThread)
	info.ContextRecord.Rcx = 0
	info.ContextRecord.Dr0 = 0
	info.ContextRecord.Dr1 = 0
	info.ContextRecord.Dr2 = 0
	info.ContextRecord.Dr3 = 0
	return exceptionContinue
}

func addVectoredExceptionHandler() (uintptr, error) {
	ret, _, err := syscall.SyscallN(ldrAPI.RtlAddVectoredExceptionHandler, 1, vehCallback)
	if ret == 0 {
		return 0, err
	}
	return ret, nil
}

func removeVectoredExceptionHandler(handle uintptr) {
	if handle != 0 {
		syscall.SyscallN(ldrAPI.RtlRemoveVectoredExceptionHandler, handle)
	}
}

func alignedContextBuffer() ([]byte, *threadContext) {
	size := unsafe.Sizeof(threadContext{})
	buf := make([]byte, size+16)
	ptr := (uintptr(unsafe.Pointer(&buf[0])) + 15) &^ 15
	return buf, (*threadContext)(unsafe.Pointer(ptr))
}

func printf(format string, a ...interface{}) {
	fmt.Printf(format, a...)
}

// PackArgs 打包 BOF 参数
func PackArgs(args []interface{}) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0))
	for _, arg := range args {
		switch v := arg.(type) {
		case uint32:
			binary.Write(buf, binary.LittleEndian, uint32(4))
			binary.Write(buf, binary.LittleEndian, v)
		case int:
			binary.Write(buf, binary.LittleEndian, uint32(4))
			binary.Write(buf, binary.LittleEndian, uint32(v))
		case string:
			strBytes := append([]byte(v), 0)
			binary.Write(buf, binary.LittleEndian, uint32(len(strBytes)))
			buf.Write(strBytes)
		case []byte:
			binary.Write(buf, binary.LittleEndian, uint32(len(v)))
			buf.Write(v)
		}
	}
	result := buf.Bytes()
	binary.LittleEndian.PutUint32(result, uint32(len(result)))
	return result
}

func isSpecialSymbol(sym *pecoff.Symbol) bool {
	return sym.StorageClass == windef.IMAGE_SYM_CLASS_EXTERNAL && sym.SectionNumber == 0
}

func isImportSymbol(sym *pecoff.Symbol) bool {
	return strings.HasPrefix(sym.NameString(), "__imp_")
}

func stripStdcallSuffix(name string) string {
	if idx := strings.Index(name, "@"); idx != -1 {
		return name[:idx]
	}
	return name
}

func amd64RelocName(relocType uint16) string {
	if name, ok := windef.MAP_IMAGE_REL_AMD64[relocType]; ok {
		return name
	}
	return fmt.Sprintf("0x%X", relocType)
}

// resolveSymbolAddress 符号解析：解析内置 API 或从 DLL 导入
func resolveSymbolAddress(symbolName string, outChannel chan<- interface{}) uintptr {
	if strings.HasPrefix(symbolName, "__imp_") {
		symbolName = symbolName[6:]
	}
	if strings.HasPrefix(symbolName, "_") {
		symbolName = symbolName[1:]
	}
	symbolName = stripStdcallSuffix(symbolName)

	if addr := resolveInternalAPI(symbolName, outChannel); addr != 0 {
		return addr
	}

	libName := ""
	procName := ""
	if parts := strings.Split(symbolName, "$"); len(parts) == 2 {
		libName, procName = parts[0], parts[1]
	} else {
		procName = symbolName
		if addr := resolveAPIFromCommonModules(procName); addr != 0 {
			return addr
		}
		return 0
	}

	if !strings.Contains(libName, ".") {
		libName += ".dll"
	}
	return resolveAPI(libName, procName)
}

// Load 加载默认方法 "go"
func Load(coffBytes []byte, argBytes []byte) (string, error) {
	return LoadWithMethod(coffBytes, argBytes, "go")
}

// LoadWithMethod 加载指定方法并执行
func LoadWithMethod(coffBytes []byte, argBytes []byte, method string) (string, error) {
	outputChan := make(chan interface{})
	parsed := pecoff.Explore(binutil.WrapByteSlice(coffBytes))
	parsed.ReadAll()
	parsed.Seal()

	if parsed.FileHeader == nil {
		return "", fmt.Errorf("missing COFF file header")
	}
	if parsed.FileHeader.Machine != windef.IMAGE_FILE_MACHINE_AMD64 {
		return "", fmt.Errorf("only AMD64 is supported")
	}

	pCoffee := &Coffee{
		Data:     coffBytes,
		Symbols:  parsed.Symbols,
		Sections: parsed.Sections.Array(),
		SecMap:   make([]SectionMap, parsed.Sections.Len()),
	}

	for _, sym := range pCoffee.Symbols {
		if isSpecialSymbol(sym) {
			if isImportSymbol(sym) {
				pCoffee.GOTSize += 8
			} else {
				pCoffee.BSSSize += sym.Value + 8
			}
		}
	}

	var currentSize uintptr
	for _, sec := range pCoffee.Sections {
		if sec.SizeOfRawData > 0 {
			currentSize += alignUp(uintptr(sec.SizeOfRawData))
		}
	}
	gotOffset := currentSize
	currentSize += alignUp(uintptr(pCoffee.GOTSize))
	bssOffset := currentSize
	currentSize += alignUp(uintptr(pCoffee.BSSSize))
	pCoffee.TotalSize = currentSize

	baseAddr, err := winVirtualAlloc(0, pCoffee.TotalSize, memCommit|memReserve|memTopDown, pageReadWrite)
	if err != nil {
		return "", err
	}
	pCoffee.ImageBase = baseAddr
	defer winVirtualFree(baseAddr)

	printf("[+] Allocated BOF buffer at %p (Size: %d)\n", unsafe.Pointer(baseAddr), pCoffee.TotalSize)

	pNextBase := baseAddr
	for i, sec := range pCoffee.Sections {
		if sec.SizeOfRawData == 0 {
			continue
		}
		pCoffee.SecMap[i] = SectionMap{Ptr: pNextBase, Size: sec.SizeOfRawData}
		copy((*[1 << 30]byte)(unsafe.Pointer(pNextBase))[:sec.SizeOfRawData], sec.RawData())
		pNextBase += alignUp(uintptr(sec.SizeOfRawData))
	}
	pCoffee.GOT = baseAddr + gotOffset
	pCoffee.BSS = baseAddr + bssOffset

	// 重定位处理
	gotIndex := 0
	bssIdx := 0
	gotMap := make(map[string]uintptr)
	bssMap := make(map[int]uintptr)

	for i, sec := range pCoffee.Sections {
		if sec.SizeOfRawData == 0 {
			continue
		}
		secAddr := pCoffee.SecMap[i].Ptr

		for _, reloc := range sec.Relocations() {
			sym := pCoffee.Symbols[reloc.SymbolTableIndex]
			if sym.StorageClass > 3 || reloc.Type == windef.IMAGE_REL_AMD64_ABSOLUTE {
				continue
			}

			var funcSlot uintptr
			var symbolSecAddr uintptr
			var bssAddr uintptr
			relocAddr := secAddr + uintptr(reloc.VirtualAddress)

			if isSpecialSymbol(sym) {
				if isImportSymbol(sym) {
					rawName := sym.NameString()
					if slot, ok := gotMap[rawName]; ok {
						funcSlot = slot
					} else {
						extAddr := resolveSymbolAddress(rawName, outputChan)
						if extAddr == 0 {
							return "", fmt.Errorf("failed to resolve: %s", rawName)
						}
						funcSlot = pCoffee.GOT + uintptr(gotIndex*8)
						*(*uintptr)(unsafe.Pointer(funcSlot)) = extAddr
						gotMap[rawName] = funcSlot
						gotIndex++
					}
				} else {
					symIndex := int(reloc.SymbolTableIndex)
					if addr, ok := bssMap[symIndex]; ok {
						bssAddr = addr
					} else {
						bssAddr = pCoffee.BSS + uintptr(bssIdx) + 4
						bssMap[symIndex] = bssAddr
						bssIdx += int(sym.Value)
					}
				}
			} else {
				if sym.SectionNumber <= 0 || int(sym.SectionNumber) > len(pCoffee.SecMap) {
					return "", fmt.Errorf("invalid section idx")
				}
				symbolSecAddr = pCoffee.SecMap[int(sym.SectionNumber-1)].Ptr
			}

			if funcSlot != 0 {
				offset := uint32(funcSlot - relocAddr - 4)
				*(*uint32)(unsafe.Pointer(relocAddr)) = offset
			} else {
				if reloc.Type >= windef.IMAGE_REL_AMD64_REL32 && reloc.Type <= windef.IMAGE_REL_AMD64_REL32_5 {
					var offset uint32
					disp := uintptr(reloc.Type - 4)
					if bssAddr != 0 {
						offset = uint32(bssAddr - disp - (relocAddr + 4))
					} else if (sym.StorageClass == windef.IMAGE_SYM_CLASS_STATIC && sym.Value != 0) || (sym.StorageClass == windef.IMAGE_SYM_CLASS_EXTERNAL && sym.SectionNumber != 0) {
						offset = uint32(uintptr(sym.Value) + symbolSecAddr - relocAddr - 4 - disp)
					} else {
						orig := *(*uint32)(unsafe.Pointer(relocAddr))
						offset = uint32(uintptr(orig) + symbolSecAddr - relocAddr - 4 - disp)
					}
					*(*uint32)(unsafe.Pointer(relocAddr)) = offset
				} else if reloc.Type == windef.IMAGE_REL_AMD64_ADDR32NB {
					var offset uint32
					if bssAddr != 0 {
						offset = uint32(bssAddr - (relocAddr + 4))
					} else if (sym.StorageClass == windef.IMAGE_SYM_CLASS_STATIC && sym.Value != 0) || (sym.StorageClass == windef.IMAGE_SYM_CLASS_EXTERNAL && sym.SectionNumber != 0) {
						offset = uint32(uintptr(sym.Value) + symbolSecAddr - relocAddr - 4)
					} else {
						orig := *(*uint32)(unsafe.Pointer(relocAddr))
						offset = uint32(uintptr(orig) + symbolSecAddr - relocAddr - 4)
					}
					*(*uint32)(unsafe.Pointer(relocAddr)) = offset
				} else if reloc.Type == windef.IMAGE_REL_AMD64_ADDR64 {
					var val uint64
					if bssAddr != 0 {
						val = uint64(bssAddr - (relocAddr + 4))
					} else if (sym.StorageClass == windef.IMAGE_SYM_CLASS_STATIC && sym.Value != 0) || (sym.StorageClass == windef.IMAGE_SYM_CLASS_EXTERNAL && sym.SectionNumber != 0) {
						val = uint64(uintptr(sym.Value) + symbolSecAddr)
					} else {
						orig := *(*uint64)(unsafe.Pointer(relocAddr))
						val = uint64(uintptr(orig) + symbolSecAddr)
					}
					*(*uint64)(unsafe.Pointer(relocAddr)) = val
				}
			}
		}

		if sec.Characteristics&imageScnMemExecute != 0 {
			var oldProtect uint32
			syscall.SyscallN(ldrAPI.VirtualProtect, secAddr, uintptr(sec.SizeOfRawData), uintptr(pageExecuteReadWrite), uintptr(unsafe.Pointer(&oldProtect)))
		}
	}

	go executeCoffeeMethod(pCoffee, method, argBytes, outputChan)

	var resultBuilder strings.Builder
	for msg := range outputChan {
		if s, ok := msg.(string); ok {
			resultBuilder.WriteString(s + "\n")
		}
	}
	return resultBuilder.String(), nil
}

func executeCoffeeMethod(pCoffee *Coffee, methodName string, args []byte, out chan<- interface{}) {
	defer close(out)
	defer func() {
		if r := recover(); r != nil {
			out <- fmt.Sprintf("[!] Exception: %v\n%s", r, debug.Stack())
		}
	}()

	var entry uintptr
	found := false
	for _, sym := range pCoffee.Symbols {
		if sym.NameString() == methodName {
			secIdx := int(sym.SectionNumber - 1)
			entry = pCoffee.SecMap[secIdx].Ptr + uintptr(sym.Value)
			found = true
			break
		}
	}

	if !found {
		out <- fmt.Sprintf("[-] Entry point '%s' not found", methodName)
		return
	}

	if len(args) == 0 {
		args = make([]byte, 1)
	}

	if err := hitCoffeeEntryPointWithThread(entry, uintptr(unsafe.Pointer(&args[0])), uintptr(len(args))); err != nil {
		out <- fmt.Sprintf("[-] Execution failed: %v", err)
	}
}

func hitCoffeeEntryPointWithThread(entry uintptr, pArg uintptr, argLen uintptr) error {
	veh, err := addVectoredExceptionHandler()
	if err != nil {
		return err
	}
	defer removeVectoredExceptionHandler(veh)

	var hThread uintptr
	ret, _, _ := syscall.SyscallN(
		ldrAPI.NtCreateThreadEx,
		uintptr(unsafe.Pointer(&hThread)),
		uintptr(threadAllAccess),
		0,
		uintptr(^uintptr(0)),
		ldrAPI.RtlExitUserThread,
		0,
		uintptr(threadCreateSuspended),
		0, 0, 0, 0,
	)
	if ret != 0 {
		return fmt.Errorf("NtCreateThreadEx error: 0x%X", ret)
	}
	defer syscall.SyscallN(ldrAPI.CloseHandle, hThread)

	_, ctx := alignedContextBuffer()
	ctx.ContextFlags = contextAll
	ret, _, _ = syscall.SyscallN(ldrAPI.NtGetContextThread, hThread, uintptr(unsafe.Pointer(ctx)))
	if ret != 0 {
		return fmt.Errorf("NtGetContextThread error: 0x%X", ret)
	}

	ctx.Rip = uint64(entry)
	ctx.Rcx = uint64(pArg)
	ctx.Rdx = uint64(argLen)
	*(*uint64)(unsafe.Pointer(uintptr(ctx.Rsp))) = uint64(ldrAPI.RtlExitUserThread)

	ret, _, _ = syscall.SyscallN(ldrAPI.NtSetContextThread, hThread, uintptr(unsafe.Pointer(ctx)))
	if ret != 0 {
		return fmt.Errorf("NtSetContextThread error: 0x%X", ret)
	}
	ret, _, _ = syscall.SyscallN(ldrAPI.NtResumeThread, hThread, 0)
	if ret != 0 {
		return fmt.Errorf("NtResumeThread error: 0x%X", ret)
	}
	ret, _, _ = syscall.SyscallN(ldrAPI.NtWaitForSingleObject, hThread, 0, 0)
	if ret != 0 {
		return fmt.Errorf("NtWaitForSingleObject error: 0x%X", ret)
	}

	return nil
}
