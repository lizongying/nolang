# Nolang VSCode Extension

VSCode 插件，为 Nolang 语言提供完整的 IDE 支持。

## 功能特性

- **语法高亮** - 支持关键字、类型、字符串、数字、函数等语法元素的着色
- **代码补全** - 基于 Language Server Protocol 的智能代码补全
- **定义跳转** - 快速跳转到变量和函数的定义位置
- **引用查找** - 查找变量和函数的所有引用
- **悬停提示** - 显示变量/函数的声明信息和类型
- **文档符号** - 显示文档结构大纲，便于快速导航

## 安装方法

### 方法一：从 VSIX 安装（推荐）

1. 下载 `vscode-nolang-0.1.0.vsix` 文件
2. 打开 VSCode
3. 打开扩展面板（快捷键：`Ctrl+Shift+X` 或 `Cmd+Shift+X`）
4. 点击扩展面板右上角的 `...` 菜单
5. 选择 `Install from VSIX...`
6. 选择下载的 `.vsix` 文件

### 方法二：从源代码构建

```bash
# 克隆项目
git clone https://github.com/lizongying/nolang.git
cd nolang/vscode-nolang

# 安装依赖
npm install

# 编译 TypeScript
npm run compile

# 创建 VSIX 包
npm run package

# 安装到 VSCode
code --install-extension vscode-nolang-0.1.0.vsix
```

## 使用方法

### 创建 Nolang 文件

1. 在 VSCode 中新建文件
2. 将文件保存为 `.no` 扩展名（例如：`main.no`）
3. VSCode 会自动识别并启用 Nolang 语言支持

### 示例代码

```nolang
// Nolang 示例代码

// 变量声明（隐式类型推断）
x = 10
name = 'Hello World'
flag = true
pi = 3.14

// 函数定义
add(a i64, b i64) {
    result = a + b
    print(result)
}

// 函数调用
add(5, 3)

// 条件语句
if x > 5 {
    print('x is greater than 5')
}

// 循环
count = 0
for count < 10 {
    print(count)
    count = count + 1
}

// 结构体定义
user {
    name str
    age i64
}

// 对象字面量
u = user {
    name: 'Alice'
    age: 25
}

// 属性访问
print(u.name)
```

### 快捷键

| 快捷键 | 功能 |
|--------|------|
| `F12` | 跳转到定义 |
| `Ctrl+F12` | 查找引用 |
| `Ctrl+Space` | 触发代码补全 |
| `Ctrl+Shift+O` | 显示文档符号 |
| `Ctrl+Hover` | 显示悬停提示 |

## 开发指南

### 环境要求

- Node.js >= 18.0.0
- VSCode >= 1.80.0
- Go >= 1.20 (用于构建 LSP 服务器)

### 项目结构

```
vscode-nolang/
├── package.json              # 插件配置
├── language-configuration.json  # 语言配置（括号配对等）
├── syntaxes/
│   └── nolang.tmLanguage.json   # TextMate 语法高亮定义
├── client/
│   ├── tsconfig.json         # TypeScript 配置
│   └── src/
│       └── extension.ts      # 扩展主入口
├── server/
│   └── nolang-lsp            # LSP 服务器可执行文件（需构建）
└── LICENSE                   # MIT 许可证
```

### 启动开发环境

1. 打开项目目录：
   ```bash
   code /path/to/vscode-nolang
   ```

2. 按 `F5` 启动扩展开发主机（Extension Development Host）

3. 在新窗口中打开一个 `.no` 文件测试功能

### 构建 LSP 服务器

LSP 服务器位于 `nolang/lsp/cmd/lsp/`，需要先构建：

```bash
cd ../src/lsp/cmd/lsp
go build -o nolang-lsp .
cp nolang-lsp ../../vscode-nolang/server/
```

### 调试 LSP 服务器

1. 在扩展开发主机窗口中打开命令面板（`Ctrl+Shift+P`）
2. 选择 `Developer: Toggle Developer Tools`
3. 在控制台中查看 LSP 日志

## 配置选项

在 VSCode 设置中搜索 `Nolang` 配置：

| 配置项 | 描述 | 默认值 |
|--------|------|--------|
| `nolang.languageServer.path` | LSP 服务器可执行文件路径 | 内置路径 |
| `nolang.languageServer.debug` | 启用调试日志 | false |

## 故障排除

### LSP 服务器无法启动

1. 确保 `server/nolang-lsp` 文件存在且有执行权限
2. 检查 VSCode 输出面板中的 "Nolang Language Server" 日志
3. 尝试手动运行 `./server/nolang-lsp` 检查是否有错误

### 语法高亮不生效

1. 确保文件扩展名是 `.no`
2. 检查右下角语言模式是否显示为 "Nolang"
3. 尝试重新加载窗口（`Ctrl+Shift+P` -> `Reload Window`）

## 支持的语法

- 变量声明（隐式类型推断和显式类型标注）
- 函数定义和调用
- 匿名函数
- 条件语句（if-else）
- 三元表达式
- 循环语句（for）
- 数组和切片
- 结构体定义和对象字面量
- 单行注释（//）

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件