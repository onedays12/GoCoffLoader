# GoCoffLoader

[English](README.md) | 中文

# 简介

随着越来越多的 C2 Implant 精简自身功能，并把很多核心功能移至 BOF 中，BOF Loader 的实现变得越来越重要。这个项目是一个实验性的 Go BOF/COFF Loader，用于学习、测试和改进 Go 中的 BOF 执行流程。

项目目前仍然不成熟，欢迎提出问题、Review 和 PR。

# 安装

```powershell
go get github.com/onedays12/GoCoffLoader@v0.1.0
```

# 使用

```go
import "github.com/onedays12/GoCoffLoader/pkg/coff"

argBytes := coff.PackArgs([]interface{}{
    "hello",
    uint32(123),
    []byte{0x41, 0x42, 0x43},
})

output, err := coff.LoadWithMethod(data, argBytes, "go")
if err != nil {
    // handle error
}
fmt.Println(output)
```

也可以使用默认入口 `go`：

```go
output, err := coff.Load(data, argBytes)
```

# 参数

BOF 入口函数仍然按常见 Beacon BOF 形式接收参数：

```c
void go(char *args, int len);
```

Go 侧传入的是已经打包好的 `[]byte`。推荐使用 `coff.PackArgs`：

```go
argBytes := coff.PackArgs([]interface{}{
    "user",
    uint32(100),
    int(200),
    []byte("raw-data"),
})

output, err := coff.LoadWithMethod(coffBytes, argBytes, "go")
```

当前 `PackArgs` 支持的类型：

- `string`：写入 `uint32(length)` + 字符串字节 + `\x00`
- `[]byte`：写入 `uint32(length)` + 原始字节
- `uint32`：写入 `uint32(4)` + 4 字节小端整数
- `int`：转为 `uint32` 后写入 `uint32(4)` + 4 字节小端整数

完整参数缓冲区结构如下：

```text
uint32 total_size
uint32 arg1_size
byte   arg1_data[arg1_size]
uint32 arg2_size
byte   arg2_data[arg2_size]
...
```

其中 `total_size` 是整个缓冲区长度，包含它自身的 4 字节。BOF 内调用 `BeaconDataParse(&parser, args, len)` 后，解析器会跳过前 4 字节，从第一个参数的 `arg_size` 开始读取。

对应关系：

- `BeaconDataExtract`：读取 `uint32 size`，返回后续 `size` 字节数据指针。
- `BeaconDataInt`：跳过当前参数的 `uint32 size`，读取后续 4 字节整数。
- `BeaconDataShort`：跳过当前参数的 `uint32 size`，读取后续 2 字节整数。

BOF 示例：

```c
void go(char *args, int len) {
    datap parser;
    BeaconDataParse(&parser, args, len);

    int userLen = 0;
    char *user = BeaconDataExtract(&parser, &userLen);
    int count = BeaconDataInt(&parser);

    BeaconPrintf(CALLBACK_OUTPUT, "user=%s count=%d", user, count);
}
```

# 注意事项

- 仅支持 Windows amd64
- BOF/COFF 在当前进程内执行
- 根据执行模式不同，原生崩溃可能会影响宿主进程

# 参考

- https://github.com/RIscRIpt/pecoff
- https://github.com/praetorian-inc/goffloader
- https://github.com/Cen4enCen/CenCoffLdr/stargazers
