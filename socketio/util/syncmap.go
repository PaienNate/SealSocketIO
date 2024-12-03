package util

import (
	"encoding/json"
	"sync"
)

// SyncMap 是一个线程安全的键值对存储结构，使用 sync.Map 实现
type SyncMap[K comparable, V any] struct {
	// m 内部使用的 sync.Map 实例
	m sync.Map
}

// Delete 删除指定键的条目
func (m *SyncMap[K, V]) Delete(key K) {
	// 调用 sync.Map 的 Delete 方法删除指定键的条目
	m.m.Delete(key)
}

// Load 获取指定键的值
func (m *SyncMap[K, V]) Load(key K) (value V, ok bool) {
	// 调用 sync.Map 的 Load 方法获取指定键的值
	v, ok := m.m.Load(key)
	if !ok {
		// 如果键不存在，返回默认值和 false
		return value, ok
	}
	// 如果键存在，返回值和 true
	return v.(V), ok
}

// Exists 检查指定键是否存在
func (m *SyncMap[K, V]) Exists(key K) bool {
	// 调用 Load 方法检查键是否存在
	_, exists := m.Load(key)
	// 返回是否存在
	return exists
}

// LoadAndDelete 获取并删除指定键的值
func (m *SyncMap[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	// 调用 sync.Map 的 LoadAndDelete 方法获取并删除指定键的值
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		// 如果键不存在，返回默认值和 false
		return value, loaded
	}
	// 如果键存在，返回值和 true
	return v.(V), loaded
}

// LoadOrStore 获取指定键的值，如果不存在则存储新值
func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	// 调用 sync.Map 的 LoadOrStore 方法获取指定键的值，如果不存在则存储新值
	a, loaded := m.m.LoadOrStore(key, value)
	// 返回实际值和是否已加载
	return a.(V), loaded
}

// Range 遍历所有键值对，执行指定的函数
func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	// 调用 sync.Map 的 Range 方法遍历所有键值对
	m.m.Range(func(key, value any) bool {
		// 将 key 和 value 转换为泛型类型 K 和 V
		return f(key.(K), value.(V))
	})
}

// Store 存储键值对
func (m *SyncMap[K, V]) Store(key K, value V) {
	// 调用 sync.Map 的 Store 方法存储键值对
	m.m.Store(key, value)
}

// Len 返回存储的键值对数量
func (m *SyncMap[K, V]) Len() int {
	// TODO: 性能优化
	// 初始化计数器
	times := 0
	// 遍历所有键值对
	m.Range(func(_ K, _ V) bool {
		// 计数器加一
		times++
		// 继续遍历
		return true
	})
	// 返回计数器的值
	return times
}

// MarshalJSON 将 SyncMap 序列化为 JSON 字节切片
func (m *SyncMap[K, V]) MarshalJSON() ([]byte, error) {
	// 创建一个临时的 map 来存储键值对
	m2 := make(map[K]V)
	// 遍历所有键值对
	m.Range(func(key K, value V) bool {
		// 将键值对存储到临时 map 中
		m2[key] = value
		// 继续遍历
		return true
	})
	// 将临时 map 序列化为 JSON 字节切片
	return json.Marshal(m2)
}

// UnmarshalJSON 将 JSON 字节切片反序列化为 SyncMap
func (m *SyncMap[K, V]) UnmarshalJSON(b []byte) error {
	// 创建一个临时的 map 来存储键值对
	m2 := make(map[K]V)
	// 尝试将 JSON 字节切片反序列化为临时 map
	err := json.Unmarshal(b, &m2)
	if err != nil {
		// 如果反序列化失败，返回错误
		return err
	}
	// 遍历临时 map 中的所有键值对
	for k, v := range m2 {
		// 将键值对存储到 SyncMap 中
		m.Store(k, v)
	}
	// 返回 nil 表示成功
	return nil
}
