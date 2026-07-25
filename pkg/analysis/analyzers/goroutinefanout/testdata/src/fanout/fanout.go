package fanout

import "sync"

func work() {}

func RuntimeRange(items []int) {
	for range items {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func RuntimeCount(count int) {
	for index := 0; index < count; index++ {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func Infinite() {
	for {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func Channel(values <-chan int) {
	for range values {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func Nested(items []int) {
	for range items {
		if len(items) > 1 {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
	}
}

func ControlFlow(items []int, value any, ready <-chan struct{}) {
	for range items {
		{
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
		if len(items) == 0 {
		} else if len(items) > 1 {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
		switch len(items) {
		case 1:
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
		switch value.(type) {
		case int:
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
		select {
		case <-ready:
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		default:
		}
	label:
		for range 1 {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			break label
		}
	}
}

func Immediate(items []int) {
	for range items {
		func() {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}()
	}
}

func Deferred(items []int) {
	for range items {
		defer func() {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}()
	}
}

func OversizedArray(values [9]int) {
	for range values {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func OversizedCount() {
	for index := 0; index < 9; index++ {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func BoundedArray(values [8]int) {
	for range values {
		go work()
	}
}

func BoundedArrayPointer(values *[8]int) {
	for range values {
		go work()
	}
}

func BoundedLiterals() {
	for range []int{1, 2, 3} {
		go work()
	}
	for range map[string]int{"one": 1, "two": 2} {
		go work()
	}
	for range "bounded" {
		go work()
	}
	for range 8 {
		go work()
	}
}

func BoundedCounts() {
	for index := 0; index < 8; index++ {
		go work()
	}
	for index := 2; index <= 8; index++ {
		go work()
	}
	for index := 8; index > 0; index-- {
		go work()
	}
}

func OversizedDescending() {
	for index := 9; index >= 1; index-- {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func NonCanonicalCounts(count int) {
	other := 0
	for index := 0; index != count; index++ {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
	for index := 0; other < count; index++ {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
	for index := 0; index < 8; other++ {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
	for index := 0; index < 8; index += 2 {
		go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
	}
}

func NestedBounds() {
	for range 2 {
		for range 3 {
			go work()
		}
	}
	for range 4 {
		for range 3 {
			go work() // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
		}
	}
	for range 0 {
		for range []int{1, 2, 3} {
			go work()
		}
	}
}

func Semaphore(items []int) {
	semaphore := make(chan struct{}, 4)
	for range items {
		semaphore <- struct{}{}
		go func() {
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func BoundarySemaphore(items []int) {
	semaphore := make(chan struct{}, 8)
	for range items {
		semaphore <- struct{}{}
		go func() {
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func ZeroSemaphore(items []int) {
	semaphore := make(chan struct{}, 0)
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func AddressedSemaphore(items []int) {
	semaphore := make(chan struct{}, 4)
	for range items {
		*(&semaphore) <- struct{}{}
		go func() {
			defer func() { <-*(&semaphore) }()
			work()
		}()
	}
}

func DeclaredSemaphore(items []int) {
	var semaphore = make(chan struct{}, 4)
	for range items {
		semaphore <- struct{}{}
		go func() {
			defer func() { <-semaphore }()
			work()
		}()
	}
}

type semaphoreFields struct {
	first  chan struct{}
	second chan struct{}
}

func DistinctSemaphoreFields(items []int) {
	var semaphores semaphoreFields
	semaphores.first = make(chan struct{}, 4)
	semaphores.second = make(chan struct{}, 4)
	for range items {
		semaphores.first <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphores.second }()
			work()
		}()
	}
}

func SameSemaphoreField(items []int) {
	var semaphores semaphoreFields
	semaphores.first = make(chan struct{}, 4)
	for range items {
		semaphores.first <- struct{}{}
		go func() {
			defer func() { <-semaphores.first }()
			work()
		}()
	}
}

func OversizedSemaphore(items []int) {
	semaphore := make(chan struct{}, 9)
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func ReassignedSemaphore(items []int) {
	semaphore := make(chan struct{}, 4)
	semaphore = make(chan struct{}, 4)
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func ConditionalSemaphore(items []int, bounded bool) {
	semaphore := make(chan struct{})
	if bounded {
		semaphore = make(chan struct{}, 4)
	}
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphore }()
			work()
		}()
	}
}

func WaitGroup(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Add(1)
		go func() {
			defer group.Done()
			work()
		}()
		group.Wait()
	}
}

func MissingWait(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Add(1)
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer group.Done()
			work()
		}()
	}
}

func WaitBeforeLaunch(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Add(1)
		group.Wait()
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer group.Done()
			work()
		}()
	}
}

func ZeroWaitGroup(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Add(0)
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer group.Done()
			work()
		}()
		group.Wait()
	}
}

func MissingDone(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Add(1)
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			work()
		}()
		group.Wait()
	}
}

type addField struct{ Add func(int) }

func NonMethodAdd(items []int) {
	value := addField{Add: func(int) {}}
	for range items {
		value.Add(1)
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			work()
		}()
	}
}

func HiddenSemaphoreRelease(items []int) {
	semaphore := make(chan struct{}, 4)
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() {
				_ = func() { <-semaphore }
			}()
			work()
		}()
	}
}

func semaphoreChannel() chan struct{} { return make(chan struct{}, 4) }

func UnidentifiedSemaphore(items []int) {
	for range items {
		semaphoreChannel() <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer func() { <-semaphoreChannel() }()
			work()
		}()
	}
}

func NonLiteralRelease(items []int) {
	semaphore := make(chan struct{}, 4)
	for range items {
		semaphore <- struct{}{}
		go func() { // want `lifecycle/unbounded-goroutine-fanout: goroutine launch is repeated without a proven limit of 8`
			defer work()
			work()
		}()
	}
}

func OutsideLoop() { go work() }

func StoredCallback(items []int) {
	callback := func() { go work() }
	for range items {
		callback()
	}
}
