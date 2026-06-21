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

1. 下载 `vscode-nolang-<version>.vsix` 文件
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
code --install-extension vscode-nolang-<version>.vsix
```

## 使用方法

### 创建 Nolang 文件

1. 在 VSCode 中新建文件
2. 将文件保存为 `.no` 扩展名（例如：`main.no`）
3. VSCode 会自动识别并启用 Nolang 语言支持

### 快捷键

| 快捷键         | 功能         |
| -------------- | ------------ |
| `F12`          | 跳转到定义   |
| `Ctrl+F12`     | 查找引用     |
| `Ctrl+Space`   | 触发代码补全 |
| `Ctrl+Shift+O` | 显示文档符号 |
| `Ctrl+Hover`   | 显示悬停提示 |

## 开发指南

### 环境要求

### 启动开发环境

1. 打开项目目录：

   ```bash
   code /path/to/vscode-nolang
   ```

2. 按 `F5` 启动扩展开发主机（Extension Development Host）

3. 在新窗口中打开一个 `.no` 文件测试功能

### 构建 LSP 服务器

```bash
cd ../
make lsp
```

### 调试 LSP 服务器

1. 在扩展开发主机窗口中打开命令面板（`Ctrl+Shift+P`）
2. 选择 `Developer: Toggle Developer Tools`
3. 在控制台中查看 LSP 日志

## 配置选项

在 VSCode 设置中搜索 `Nolang` 配置：

| 配置项                        | 描述                     | 默认值   |
| ----------------------------- | ------------------------ | -------- |
| `nolang.languageServer.path`  | LSP 服务器可执行文件路径 | 内置路径 |
| `nolang.languageServer.debug` | 启用调试日志             | false    |

## 故障排除

### LSP 服务器无法启动

1. 确保 `server/lsp` 文件存在且有执行权限
2. 检查 VSCode 输出面板中的 "Nolang Language Server" 日志
3. 尝试手动运行 `./server/lsp` 检查是否有错误

### 语法高亮不生效

1. 确保文件扩展名是 `.no`
2. 检查右下角语言模式是否显示为 "Nolang"
3. 尝试重新加载窗口（`Ctrl+Shift+P` -> `Reload Window`）

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件
