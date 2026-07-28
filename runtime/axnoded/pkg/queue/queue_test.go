package queue

import (
	"fmt"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	queue1 := New[int](0)
	queue2 := New[string]("empty")

	if queue1.Pop() != 0 {
		t.Errorf("queue1 pop error")
	}

	if queue2.Pop() != "empty" {
		t.Errorf("queue2 pop error")
	}

	queue1.Push(1)
	queue2.Push("one")

	if queue1.Get(0) != 1 {
		t.Errorf("queue1 get error")
	}

	if queue2.Get(0) != "one" {
		t.Errorf("queue2 get error")
	}

	if queue1.Pop() != 1 {
		t.Errorf("queue1 pop error")
	}

	if queue2.Pop() != "one" {
		t.Errorf("queue2 pop error")
	}

	if queue1.Pop() != 0 {
		t.Errorf("queue1 pop error")
	}

	if queue2.Pop() != "empty" {
		t.Errorf("queue2 pop error")
	}
}

func BenchmarkQueueOp(b *testing.B) {
	operationQps := []int{
		100,
		500,
		10000,
		300000,
	}
	b.ResetTimer()
	for _, qps := range operationQps {
		b.StopTimer()
		b.Run(fmt.Sprintf("qps: %v", qps), func(b *testing.B) {
			queue1 := New[int](0)
			for i := 0; i < 1000; i++ {
				queue1.Push(i)
			}
			if queue1.Length() != 1000 {
				b.Errorf("queue1 length error")
			}

			popResult := make([]int, 0)

			b.StartTimer()
			var opsWg sync.WaitGroup
			opsWg.Add(4)
			go func(queue *Queue[int], res []int) {
				defer opsWg.Done()

				var internalWg sync.WaitGroup
				for i := 0; i < qps; i++ {
					internalWg.Add(1)
					go func(queue *Queue[int]) {
						defer internalWg.Done()
						res = append(res, queue.Pop())
					}(queue)
				}
				internalWg.Wait()
			}(queue1, popResult)

			b.StopTimer()
			resMap := make(map[int]int)
			for _, v := range popResult {
				resMap[v]++
			}
			for _, v := range resMap {
				if v != 1 {
					b.Errorf("queue1 result error")
				}
			}
			b.StartTimer()

			go func(queue *Queue[int]) {
				defer opsWg.Done()

				var internalWg sync.WaitGroup
				for i := 0; i < qps; i++ {
					internalWg.Add(1)
					go func(queue *Queue[int]) {
						defer internalWg.Done()
						queue.Top()
					}(queue)
				}
				internalWg.Wait()
			}(queue1)

			go func(queue *Queue[int]) {
				defer opsWg.Done()

				var internalWg sync.WaitGroup
				for i := 0; i < qps; i++ {
					internalWg.Add(1)
					go func(queue *Queue[int]) {
						defer internalWg.Done()
						queue.Length()
					}(queue)
				}
				internalWg.Wait()
			}(queue1)

			go func(queue *Queue[int]) {
				defer opsWg.Done()

				var internalWg sync.WaitGroup
				for i := 0; i < qps; i++ {
					internalWg.Add(1)
					go func(queue *Queue[int], num int) {
						defer internalWg.Done()
						queue.Push(num)
					}(queue, i)
				}
				internalWg.Wait()
			}(queue1)
			opsWg.Wait()
		})
	}
}
