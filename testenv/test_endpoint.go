package main

import (
"fmt"
"net/http"
)

func main() {
client := &http.Client{}
h := &harness{client: client}

resp := &cardProductsResponse{}
path := "/cards/v1/card-applications/test-app/card-products"
headers := map[string]string{}

fmt.Printf("Testing path: %s\n", mockGatehubURL+path)
err := h.getJSON(path, resp, headers)
if err != nil {
fmt.Printf("ERROR: %v\n", err)
return
}

fmt.Printf("SUCCESS: Got %d products\n", len(resp.Data))
for _, p := range resp.Data {
fmt.Printf("  - %s (%s)\n", p.Name, p.Code)
}
}
