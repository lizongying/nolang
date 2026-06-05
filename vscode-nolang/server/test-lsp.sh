#!/bin/bash

# 發送 initialize 請求
echo -e "Content-Length: 54\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"capabilities\":{}}}"

# 發送 initialized 通知
echo -e "Content-Length: 48\r\n\r\n{\"jsonrpc\":\"2.0\",\"method\":\"initialized\",\"params\":{}}"

# 發送格式化請求
echo -e "Content-Length: 105\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"textDocument/formatting\",\"params\":{\"textDocument\":{\"uri\":\"file:///test.no\"},\"options\":{\"tabSize\":4,\"insertSpaces\":true}}}"

# 發送 shutdown
echo -e "Content-Length: 40\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"shutdown\",\"params\":{}}"

# 發送 exit
echo -e "Content-Length: 36\r\n\r\n{\"jsonrpc\":\"2.0\",\"method\":\"exit\",\"params\":{}}"