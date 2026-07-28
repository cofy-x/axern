package queue

import "sync"

const minQueueLen = 64

type Queue[T comparable] struct {
	buf               []*T
	bufMap            map[T]struct{}
	empty             T
	head, tail, count int
	sync.Mutex
}

func New[T comparable](emptyObj T) *Queue[T] {
	return &Queue[T]{
		buf:    make([]*T, minQueueLen),
		bufMap: make(map[T]struct{}, minQueueLen),
		empty:  emptyObj,
	}
}

func (q *Queue[T]) Length() int {
	q.Lock()
	defer q.Unlock()

	return q.count
}

func (q *Queue[T]) resize() {
	newBuf := make([]*T, q.count<<1)

	if q.tail > q.head {
		copy(newBuf, q.buf[q.head:q.tail])
	} else {
		n := copy(newBuf, q.buf[q.head:])
		copy(newBuf[n:], q.buf[:q.tail])
	}

	q.head = 0
	q.tail = q.count
	q.buf = newBuf
}

func (q *Queue[T]) pushUnlocked(elem T) {
	if q.count == len(q.buf) {
		q.resize()
	}

	q.bufMap[elem] = struct{}{}
	q.buf[q.tail] = &elem
	q.tail = (q.tail + 1) & (len(q.buf) - 1)
	q.count++
}

func (q *Queue[T]) Push(elem T) {
	q.Lock()
	defer q.Unlock()

	if !q.hasUnlocked(elem) {
		q.pushUnlocked(elem)
	}
}

func (q *Queue[T]) hasUnlocked(elem T) bool {
	_, has := q.bufMap[elem]
	return has
}

func (q *Queue[T]) Has(elem T) bool {
	q.Lock()
	defer q.Unlock()

	return q.hasUnlocked(elem)
}

func (q *Queue[T]) Top() T {
	q.Lock()
	defer q.Unlock()
	if q.count <= 0 {
		return q.empty
	}
	return *(q.buf[q.head])
}

func (q *Queue[T]) Get(i int) T {
	q.Lock()
	defer q.Unlock()
	if i < 0 {
		i += q.count
	}
	if i < 0 || i >= q.count {
		return q.empty
	}
	return *(q.buf[(q.head+i)&(len(q.buf)-1)])
}

func (q *Queue[T]) Pop() T {
	q.Lock()
	defer q.Unlock()
	if q.count <= 0 {
		return q.empty
	}
	ret := q.buf[q.head]
	q.buf[q.head] = nil
	q.head = (q.head + 1) & (len(q.buf) - 1)
	q.count--
	if len(q.buf) > minQueueLen && (q.count<<2) == len(q.buf) {
		q.resize()
	}

	delete(q.bufMap, *ret)

	return *ret
}

func (q *Queue[T]) List() []T {
	q.Lock()
	defer q.Unlock()

	list := make([]T, 0, q.count)
	for i := 0; i < q.count; i++ {
		list = append(list, *q.buf[(q.head+i)&(len(q.buf)-1)])
	}
	return list
}

func (q *Queue[T]) Num() int {
	return q.Length()
}
