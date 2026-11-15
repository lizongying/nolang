import * as path from "path";
import * as vscode from "vscode";
import {
  LanguageClient,
  TransportKind,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

// fmtSettings mirrors FmtConfig in src/lsp/fmt_config.go. Defaults here must
// stay in sync with DefaultFmtConfig() (all true = same rewrites as `no fmt`).
function fmtSettings() {
  const c = vscode.workspace.getConfiguration("nolang.fmt");
  return {
    rewrite: c.get<boolean>("rewrite", true),
    redundantTypes: c.get<boolean>("redundantTypes", true),
    ineffectiveOverflow: c.get<boolean>("ineffectiveOverflow", true),
    stringConcat: c.get<boolean>("stringConcat", true),
    crossModuleSignatures: c.get<boolean>("crossModuleSignatures", true),
  };
}

export function activate(context: vscode.ExtensionContext) {
  const binName = process.platform === "win32" ? "lsp.exe" : "lsp";
  const serverPath = context.asAbsolutePath(path.join("server", binName));

  const outputChannel = vscode.window.createOutputChannel(
    "Nolang Language Server",
  );
  outputChannel.appendLine(`Server path: ${serverPath}`);
  outputChannel.show();

  const serverOptions: ServerOptions = {
    run: {
      command: serverPath,
      transport: TransportKind.stdio,
      args: [],
    },
    debug: {
      command: serverPath,
      transport: TransportKind.stdio,
      args: [],
    },
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "nolang" }],
    // Sent with `initialize`; the server reads initializationOptions.fmt.
    initializationOptions: { fmt: fmtSettings() },
    outputChannel: outputChannel,
    // 添加 middleware 來偵錯
    middleware: {
      handleDiagnostics: (uri, diagnostics, next) => {
        outputChannel.appendLine(
          `Diagnostics for ${uri}: ${diagnostics.length} issues`,
        );
        return next(uri, diagnostics);
      },
    },
  };

  client = new LanguageClient(
    "nolang",
    "Nolang Language Server",
    serverOptions,
    clientOptions,
  );

  // 使用 onDidChangeState 監聽狀態變化（替代 onReady）
  client.onDidChangeState((event) => {
    outputChannel.appendLine(`State changed: ${event.newState}`);
    if (event.newState === 2) {
      // 2 = Running
      outputChannel.appendLine("✅ Language client is running");
      // 獲取 server 能力
      try {
        const capabilities = (client as any)._serverCapabilities;
        if (capabilities) {
          outputChannel.appendLine(
            `Server capabilities: ${JSON.stringify(capabilities, null, 2)}`,
          );
          if (capabilities.documentFormattingProvider) {
            outputChannel.appendLine(
              "✅ Document formatting provider is enabled",
            );
          } else {
            outputChannel.appendLine(
              "❌ Document formatting provider is NOT enabled",
            );
          }
        }
      } catch (err) {
        outputChannel.appendLine(`Error getting capabilities: ${err}`);
      }
    }
  });

  // 啟動 client
  client
    .start()
    .then(() => {
      outputChannel.appendLine("Client start initiated");
    })
    .catch((err) => {
      outputChannel.appendLine(`Failed to start client: ${err.message}`);
    });

  // Apply setting changes without restarting the server.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("nolang.fmt") || !client) {
        return;
      }
      void client.sendNotification("workspace/didChangeConfiguration", {
        settings: { fmt: fmtSettings() },
      });
    }),
  );

  context.subscriptions.push(client);
}

export function deactivate(): Thenable<void> | undefined {
  if (!client) {
    return undefined;
  }
  return client.stop();
}
