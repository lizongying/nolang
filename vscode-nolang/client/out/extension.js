"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const path = __importStar(require("path"));
const vscode = __importStar(require("vscode"));
const node_1 = require("vscode-languageclient/node");
let client;
function activate(context) {
    const binName = process.platform === 'win32' ? 'nolang-lsp.exe' : 'nolang-lsp';
    const serverPath = context.asAbsolutePath(path.join('server', binName));
    const outputChannel = vscode.window.createOutputChannel('Nolang Language Server');
    outputChannel.appendLine(`Server path: ${serverPath}`);
    outputChannel.show();
    const serverOptions = {
        run: {
            command: serverPath,
            transport: node_1.TransportKind.stdio,
            args: []
        },
        debug: {
            command: serverPath,
            transport: node_1.TransportKind.stdio,
            args: []
        }
    };
    const clientOptions = {
        documentSelector: [{ scheme: 'file', language: 'nolang' }],
        outputChannel: outputChannel,
        // 添加 middleware 來偵錯
        middleware: {
            handleDiagnostics: (uri, diagnostics, next) => {
                outputChannel.appendLine(`Diagnostics for ${uri}: ${diagnostics.length} issues`);
                return next(uri, diagnostics);
            }
        }
    };
    client = new node_1.LanguageClient('nolang', 'Nolang Language Server', serverOptions, clientOptions);
    // 使用 onDidChangeState 監聽狀態變化（替代 onReady）
    client.onDidChangeState((event) => {
        outputChannel.appendLine(`State changed: ${event.newState}`);
        if (event.newState === 2) { // 2 = Running
            outputChannel.appendLine('✅ Language client is running');
            // 獲取 server 能力
            try {
                const capabilities = client._serverCapabilities;
                if (capabilities) {
                    outputChannel.appendLine(`Server capabilities: ${JSON.stringify(capabilities, null, 2)}`);
                    if (capabilities.documentFormattingProvider) {
                        outputChannel.appendLine('✅ Document formatting provider is enabled');
                    }
                    else {
                        outputChannel.appendLine('❌ Document formatting provider is NOT enabled');
                    }
                }
            }
            catch (err) {
                outputChannel.appendLine(`Error getting capabilities: ${err}`);
            }
        }
    });
    // 啟動 client
    client.start().then(() => {
        outputChannel.appendLine('Client start initiated');
    }).catch((err) => {
        outputChannel.appendLine(`Failed to start client: ${err.message}`);
    });
    context.subscriptions.push(client);
}
function deactivate() {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
