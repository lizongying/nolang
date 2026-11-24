#!/usr/bin/env bash
# 开 PR 前对作者收录记录做只读自查。
# 用法：precheck.sh <repos/github.com/<owner>.noml> [nohub 仓库根目录]
# 检查：文件存在、noml 顶层是数组、元素短名非空且不重复；网络项打印手工验证命令。
# 不修改任何文件，不执行 git 写操作。

set -euo pipefail

FILE="${1:-}"
ROOT="${2:-/Users/lizongying/IdeaProjects/nohub}"

if [ -z "$FILE" ]; then
  echo "用法: precheck.sh <repos/github.com/<owner>.noml> [nohub 仓库根目录]" >&2
  exit 2
fi

case "$FILE" in
  repos/*/*.noml) ;;
  *) echo "错误: 路径须为 repos/<host>/<owner>.noml，实际: $FILE" >&2; exit 2 ;;
esac

FULL="$ROOT/$FILE"
[ -f "$FULL" ] || { echo "错误: 文件不存在: $FULL" >&2; exit 1; }

# owner = 文件名去掉 .noml
BASE=$(basename "$FILE" .noml)
HOST=$(basename "$(dirname "$FILE")")

echo "== 作者记录自查 =="
echo "文件: $FILE"
echo "推导 owner: $BASE  host: $HOST"

# 用 node + nohub 自带 noml 解析器（若可用）做结构检查；否则退回文本启发式。
NODE="$ROOT/lib/noml.mjs"
if command -v node >/dev/null 2>&1 && [ -f "$NODE" ]; then
  node --input-type=module - "$FULL" "$NODE" <<'JS'
import fs from 'node:fs';
const [file, mod] = process.argv.slice(2);
const { parseNoml } = await import('file://' + mod);
let src = fs.readFileSync(file, 'utf8');
let data;
try { data = parseNoml(src); } catch (e) { console.error('noml 解析失败:', e.message); process.exit(1); }
if (!Array.isArray(data)) { console.error('顶层必须是数组'); process.exit(1); }
const names = [];
let bad = 0;
for (const el of data) {
  const name = typeof el === 'string' ? el : (el && typeof el === 'object' ? el.name : undefined);
  if (typeof name !== 'string' || name.length === 0) { console.error('元素缺少有效短名:', JSON.stringify(el)); bad++; continue; }
  if (!/^[A-Za-z0-9._-]+$/.test(name)) { console.error('短名含非法字符:', name); bad++; }
  names.push(name);
}
const dup = names.filter((n, i) => names.indexOf(n) !== i);
if (dup.length) { console.error('重复短名:', [...new Set(dup)].join(', ')); bad++; }
console.log('条目数:', names.length, '短名:', names.join(', ') || '(空)');
process.exit(bad ? 1 : 0);
JS
else
  echo "提示: 未找到 node 或 $NODE，跳过 noml 结构解析。"
fi

echo
echo "== 需网络确认的项（在能访问 GitHub 的环境手工执行）=="
for name in $(grep -oE "'[A-Za-z0-9._-]+'" "$FULL" | tr -d "'" | sort -u); do
  echo "仓库: https://$HOST/$BASE/$name"
  echo "  workspace: https://raw.githubusercontent.com/$BASE/$name/HEAD/workspace.noml"
done
echo
echo "作者校验: PR 必须由 GitHub login = $BASE 的账号发起。"
