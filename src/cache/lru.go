// Package cache 提供线程安全的、有容量上限的 LRU 缓存，以及基于内容寻址的
// 缓存键构造器。
//
// 设计动机（见编译器缓存重构 2026-08-02）：
//   - 旧的 token / AST 缓存以「文件路径」为键，且全局只增不删。这在常驻服务
//     （LSP / fmt）里有两个潜伏陷阱：① 同路径内容变化后拿到过期结果；② 缓存
//     无淘汰上限，进程只增不删会内存泄漏。
//   - 改为「(路径, 内容哈希)」复合键后，同路径内容变化会自动产生新键 → 无过期；
//     配合 LRU 上限，常驻进程内存有界。
//
// 值是不可变的（token 切片 / 只读 AST），LRU 淘汰只丢弃引用，由 GC 回收，
// 不复制底层数据。
package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// LRU 是线程安全的、有容量上限的 LRU 缓存。
// V 应为不可变值（或调用方保证不共享可变状态）。
type LRU[V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element
	ll       *list.List // 队首最旧，队尾最新
}

type lruEntry[V any] struct {
	key string
	val V
}

// NewLRU 创建容量为 capacity 的 LRU。capacity <= 0 时回退为 1024。
// 容量只决定淘汰上限，不影响正确性：淘汰后得到的是「重新计算」，而非错误结果。
func NewLRU[V any](capacity int) *LRU[V] {
	if capacity <= 0 {
		capacity = 1024
	}
	return &LRU[V]{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		ll:       list.New(),
	}
}

// Get 命中则将元素移到队尾（标记为最近使用），返回值与 true；未命中返回零值与 false。
func (c *LRU[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToBack(el)
		return el.Value.(*lruEntry[V]).val, true
	}
	var zero V
	return zero, false
}

// Put 写入或更新键值，并在超出容量时淘汰最旧元素。
func (c *LRU[V]) Put(key string, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry[V]).val = val
		c.ll.MoveToBack(el)
		return
	}
	if c.ll.Len() >= c.capacity {
		if front := c.ll.Front(); front != nil {
			old := front.Value.(*lruEntry[V])
			delete(c.items, old.key)
			c.ll.Remove(front)
		}
	}
	el := c.ll.PushBack(&lruEntry[V]{key: key, val: val})
	c.items[key] = el
}

// Clear 清空全部条目（命令级 cache 重置时调用）。
func (c *LRU[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element, c.capacity)
	c.ll.Init()
}

// Key 由「路径 + 内容」构造内容寻址的缓存键：
// 同路径、内容不同 → 不同键 → 不会命中过期结果；同路径同内容 → 同键 → 命中。
// 分隔符用 NUL，路径中不会出现该字节。
func Key(path, content string) string {
	sum := sha256.Sum256([]byte(content))
	return path + "\x00" + hex.EncodeToString(sum[:])
}
