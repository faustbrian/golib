package httptimeout

import (
	httpalias "net/http"
	"time"
)

func clients(timeout time.Duration) {
	_ = &httpalias.Client{}                     // want `http/client-timeout: http.Client must declare a positive Timeout`
	_ = httpalias.Client{Timeout: 0}            // want `http/client-timeout: http.Client Timeout must be positive`
	_ = httpalias.Client{Timeout: -time.Second} // want `http/client-timeout: http.Client Timeout must be positive`
	_ = httpalias.Client{Timeout: time.Second}
	_ = httpalias.Client{Timeout: timeout}
	_ = new(httpalias.Client) // want `http/client-timeout: new\(http.Client\) has no explicit timeout policy`

	var zero httpalias.Client // want `http/client-timeout: zero-value http.Client has no explicit timeout policy`
	_ = zero
	var pointer *httpalias.Client
	_ = pointer

	type LocalClient httpalias.Client
	_ = &LocalClient{}
	_ = httpalias.Client{nil, nil, nil, time.Second}

	client := &httpalias.Client{Timeout: time.Second}
	_, _ = client.Do(nil)
	helper(1)
	_ = len([]int{})
	_ = make([]int, 0, 1)
	_ = new(int)
}

func helper(int) {}
