package coff

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	cpACP             = 0
	mbErrInvalidChars = 0x8
)

var (
	beaconStoreMu sync.Mutex
	beaconStore   = make(map[string]uintptr)
)

type beaconDataParser struct {
	Original uintptr
	Buffer   uintptr
	Length   int32
	Size     int32
}

type beaconFormat struct {
	Original uintptr
	Buffer   uintptr
	Length   int32
	Size     int32
}

func resolveInternalAPI(symbolName string, outChannel chan<- interface{}) uintptr {
	switch symbolName {
	case "BeaconOutput":
		return syscall.NewCallback(beaconOutputForChannel(outChannel))
	case "BeaconPrintf":
		return syscall.NewCallback(beaconPrintfForChannel(outChannel))
	case "BeaconDataInt":
		return syscall.NewCallback(beaconDataInt)
	case "BeaconDataShort":
		return syscall.NewCallback(beaconDataShort)
	case "BeaconDataParse":
		return syscall.NewCallback(beaconDataParse)
	case "BeaconDataExtract":
		return syscall.NewCallback(beaconDataExtract)
	case "BeaconDataLength":
		return syscall.NewCallback(beaconDataLength)
	case "BeaconFormatAlloc":
		return syscall.NewCallback(beaconFormatAlloc)
	case "BeaconFormatFree":
		return syscall.NewCallback(beaconFormatFree)
	case "BeaconFormatInt":
		return syscall.NewCallback(beaconFormatInt)
	case "BeaconFormatPrintf":
		return syscall.NewCallback(beaconFormatPrintf)
	case "BeaconFormatToString":
		return syscall.NewCallback(beaconFormatToString)
	case "BeaconIsAdmin":
		return syscall.NewCallback(beaconIsAdmin)
	case "BeaconAddValue":
		return syscall.NewCallback(beaconAddValue)
	case "BeaconGetValue":
		return syscall.NewCallback(beaconGetValue)
	case "BeaconRemoveValue":
		return syscall.NewCallback(beaconRemoveValue)
	case "toWideChar":
		return syscall.NewCallback(toWideChar)
	}
	return 0
}

func beaconOutputForChannel(channel chan<- interface{}) func(int, uintptr, int) uintptr {
	return func(beaconType int, data uintptr, length int) uintptr {
		if data == 0 || length <= 0 {
			return 0
		}

		channel <- string(readBytes(data, length))
		return 1
	}
}

func beaconPrintfForChannel(channel chan<- interface{}) func(int, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr {
	return func(beaconType int, data uintptr, arg0 uintptr, arg1 uintptr, arg2 uintptr, arg3 uintptr, arg4 uintptr, arg5 uintptr, arg6 uintptr, arg7 uintptr, arg8 uintptr, arg9 uintptr) uintptr {
		if data == 0 {
			return 0
		}

		channel <- renderCFormat(readCString(data), []uintptr{arg0, arg1, arg2, arg3, arg4, arg5, arg6, arg7, arg8, arg9})
		return 0
	}
}

func beaconDataParse(parser *beaconDataParser, buffer uintptr, size int32) uintptr {
	if parser == nil {
		return 0
	}

	parser.Original = buffer
	parser.Buffer = buffer + 4
	parser.Length = size - 4
	parser.Size = size - 4
	return 0
}

func beaconDataInt(parser *beaconDataParser) uintptr {
	if parser == nil || parser.Length < 4 {
		return 0
	}

	value := *(*uint32)(unsafe.Pointer(parser.Buffer + 4))
	parser.Buffer += 8
	parser.Length -= 8
	return uintptr(value)
}

func beaconDataShort(parser *beaconDataParser) uintptr {
	if parser == nil || parser.Length < 2 {
		return 0
	}

	value := *(*uint16)(unsafe.Pointer(parser.Buffer + 4))
	parser.Buffer += 6
	parser.Length -= 6
	return uintptr(value)
}

func beaconDataLength(parser *beaconDataParser) uintptr {
	if parser == nil {
		return 0
	}
	return uintptr(uint32(parser.Length))
}

func beaconDataExtract(parser *beaconDataParser, sizePtr uintptr) uintptr {
	if parser == nil || parser.Length < 4 {
		return 0
	}

	length := *(*uint32)(unsafe.Pointer(parser.Buffer))
	parser.Buffer += 4
	parser.Length -= 4

	data := parser.Buffer
	parser.Buffer += uintptr(length)
	parser.Length -= int32(length)

	if sizePtr != 0 {
		*(*uint32)(unsafe.Pointer(sizePtr)) = length
	}
	return data
}

func beaconFormatAlloc(format *beaconFormat, maxSize int32) uintptr {
	if format == nil || maxSize <= 0 {
		return 0
	}

	ptr := heapAlloc(uintptr(maxSize))
	if ptr == 0 {
		return 0
	}

	format.Original = ptr
	format.Buffer = ptr
	format.Length = 0
	format.Size = maxSize
	return 0
}

func beaconFormatFree(format *beaconFormat) uintptr {
	if format == nil {
		return 0
	}

	if format.Original != 0 {
		zeroMemory(format.Original, int(format.Length))
		heapFree(format.Original)
	}

	format.Original = 0
	format.Buffer = 0
	format.Length = 0
	format.Size = 0
	return 0
}

func beaconFormatInt(format *beaconFormat, value int32) uintptr {
	if format == nil || format.Buffer == 0 || format.Length+4 > format.Size {
		return 0
	}

	out := uint32(value)
	swapped := (out>>24)&0xff | (out>>8)&0xff00 | (out<<8)&0xff0000 | (out<<24)&0xff000000
	*(*uint32)(unsafe.Pointer(format.Buffer)) = swapped
	format.Buffer += 4
	format.Length += 4
	return 0
}

func beaconFormatPrintf(format *beaconFormat, fmtPtr uintptr, a0, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) uintptr {
	if format == nil || format.Buffer == 0 || fmtPtr == 0 {
		return 0
	}

	rendered := renderCFormat(readCString(fmtPtr), []uintptr{a0, a1, a2, a3, a4, a5, a6, a7, a8, a9})
	if format.Length+int32(len(rendered)) > format.Size {
		return 0
	}

	copy((*[1 << 30]byte)(unsafe.Pointer(format.Buffer))[:len(rendered)], []byte(rendered))
	format.Buffer += uintptr(len(rendered))
	format.Length += int32(len(rendered))
	if format.Length < format.Size {
		*(*byte)(unsafe.Pointer(format.Buffer)) = 0
	}
	return 0
}

func beaconFormatToString(format *beaconFormat, sizePtr uintptr) uintptr {
	if format == nil {
		return 0
	}

	if sizePtr != 0 {
		*(*uint32)(unsafe.Pointer(sizePtr)) = uint32(format.Length)
	}
	if format.Buffer != 0 && format.Length < format.Size {
		*(*byte)(unsafe.Pointer(format.Buffer)) = 0
	}
	return format.Original
}

func beaconIsAdmin() uintptr {
	return 1
}

func beaconAddValue(key uintptr, ptr uintptr) uintptr {
	name := readCString(key)
	beaconStoreMu.Lock()
	beaconStore[name] = ptr
	beaconStoreMu.Unlock()
	return 1
}

func beaconGetValue(key uintptr) uintptr {
	name := readCString(key)
	beaconStoreMu.Lock()
	value := beaconStore[name]
	beaconStoreMu.Unlock()
	return value
}

func beaconRemoveValue(key uintptr) uintptr {
	name := readCString(key)
	beaconStoreMu.Lock()
	_, ok := beaconStore[name]
	delete(beaconStore, name)
	beaconStoreMu.Unlock()
	if ok {
		return 1
	}
	return 0
}

func toWideChar(src uintptr, dst uintptr, max int32) uintptr {
	if src == 0 || dst == 0 || max < 2 {
		return 0
	}

	ret, _, _ := syscall.SyscallN(
		ldrAPI.MultiByteToWideChar,
		uintptr(cpACP),
		uintptr(mbErrInvalidChars),
		src,
		uintptr(^uint32(0)),
		dst,
		uintptr(max/2),
	)
	return ret
}

func heapAlloc(size uintptr) uintptr {
	heap, _, _ := syscall.SyscallN(ldrAPI.GetProcessHeap)
	if heap == 0 {
		return 0
	}

	ptr, _, _ := syscall.SyscallN(ldrAPI.HeapAlloc, heap, 0x8, size)
	return ptr
}

func heapFree(ptr uintptr) {
	if ptr == 0 {
		return
	}

	heap, _, _ := syscall.SyscallN(ldrAPI.GetProcessHeap)
	if heap != 0 {
		syscall.SyscallN(ldrAPI.HeapFree, heap, 0, ptr)
	}
}

func zeroMemory(ptr uintptr, size int) {
	if ptr == 0 || size <= 0 {
		return
	}

	buf := (*[1 << 30]byte)(unsafe.Pointer(ptr))[:size]
	for i := range buf {
		buf[i] = 0
	}
}

func readCString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}

	var b strings.Builder
	for offset := uintptr(0); ; offset++ {
		ch := *(*byte)(unsafe.Pointer(ptr + offset))
		if ch == 0 {
			break
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func readWString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}

	var b strings.Builder
	for offset := uintptr(0); ; offset += 2 {
		ch := *(*uint16)(unsafe.Pointer(ptr + offset))
		if ch == 0 {
			break
		}
		b.WriteRune(rune(ch))
	}
	return b.String()
}

func readBytes(ptr uintptr, length int) []byte {
	if ptr == 0 || length <= 0 {
		return nil
	}

	out := make([]byte, length)
	copy(out, (*[1 << 30]byte)(unsafe.Pointer(ptr))[:length])
	return out
}

func renderCFormat(format string, args []uintptr) string {
	var out strings.Builder
	argIndex := 0

	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i == len(format)-1 {
			out.WriteByte(format[i])
			continue
		}

		i++
		if format[i] == '%' {
			out.WriteByte('%')
			continue
		}

		for i < len(format) && strings.ContainsAny(format[i:i+1], "0-+ #0123456789.*hljztL") {
			i++
		}
		if i >= len(format) {
			break
		}

		arg := uintptr(0)
		if argIndex < len(args) {
			arg = args[argIndex]
			argIndex++
		}

		switch format[i] {
		case 's':
			s := readCString(arg)
			if len(s) < 5 {
				if wide := readWString(arg); wide != "" {
					s = wide
				}
			}
			out.WriteString(s)
		case 'd', 'i':
			out.WriteString(fmt.Sprintf("%d", int32(arg)))
		case 'u':
			out.WriteString(fmt.Sprintf("%d", uint32(arg)))
		case 'x':
			out.WriteString(fmt.Sprintf("%x", uint32(arg)))
		case 'X':
			out.WriteString(fmt.Sprintf("%X", uint32(arg)))
		case 'p':
			out.WriteString(fmt.Sprintf("%x", unsafe.Pointer(arg)))
		case 'c':
			out.WriteByte(byte(arg))
		default:
			out.WriteString(fmt.Sprintf("%%%c", format[i]))
		}
	}

	return out.String()
}
