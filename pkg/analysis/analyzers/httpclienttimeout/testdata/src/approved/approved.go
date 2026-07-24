package approved

import "net/http"

func client() {
	_ = &http.Client{}
	_ = new(http.Client)
	var zero http.Client
	_ = zero
}
