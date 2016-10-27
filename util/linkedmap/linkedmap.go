package linkedmap

import (
	"container/list"
	"sync"
)

type LinkedMap struct {
	link list.List
	m    map[interface{}]*mValue
	lock sync.RWMutex
}

type mValue struct {
	e     *list.Element
	value interface{}
}

func New() *LinkedMap {
	return &LinkedMap{m: map[interface{}]*mValue{}}
}

func (l *LinkedMap) Append(key, value interface{}) bool {
	l.lock.Lock()
	defer l.lock.Unlock()

	if mv, found := l.m[key]; found {
		mv.value = value
		return true
	}

	mv := &mValue{e: l.link.PushBack(key), value: value}
	l.m[key] = mv

	return true
}

func (l *LinkedMap) Remove(key interface{}) bool {
	l.lock.Lock()
	defer l.lock.Unlock()

	if mv, found := l.m[key]; found {
		delete(l.m, key)
		l.link.Remove(mv.e)
	} else {
		return false
	}

	return true
}

func (l *LinkedMap) Has(key interface{}) (found bool) {
	l.lock.RLock()
	_, found = l.m[key]
	l.lock.RUnlock()

	return
}

func (l *LinkedMap) Get(key interface{}) (value interface{}, found bool) {
	l.lock.RLock()
	defer l.lock.RUnlock()

	if mv, found := l.m[key]; found {
		return mv.value, found
	} else {
		return nil, false
	}
}

func (l *LinkedMap) Each(fun func(key, value interface{}) error) error {
	l.lock.RLock()
	defer l.lock.RUnlock()

	for e := l.link.Front(); e != nil; e = e.Next() {
		if err := fun(e.Value, l.m[e.Value].value); err != nil {
			return err
		}
	}
	return nil
}
