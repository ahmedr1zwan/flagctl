package api_test

import (
	"encoding/json"
	"testing"
)

func TestFlagResponseID(t *testing.T) {
	a := newAPI(t, nil)
	for _, environment := range []string{"dev", "prod"} {
		collection := "/v1/environments/" + environment + "/flags"
		want := environment + "/checkout"
		assertID := func(response reply, status int) {
			t.Helper()
			readFlag(t, response, status)
			var body struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(response.body, &body); err != nil {
				t.Fatal(err)
			}
			if body.ID != want {
				t.Fatalf("response ID = %q, want %q", body.ID, want)
			}
		}
		assertID(a.request(t, "POST", collection, `{"key":"checkout"}`, nil), 201)
		assertID(a.request(t, "GET", collection+"/checkout", "", nil), 200)
		assertID(a.request(t, "PATCH", collection+"/checkout", `{"enabled":true,"description":"updated"}`, nil), 200)
		response := a.request(t, "GET", collection, "", nil)
		readList(t, response)
		var body struct {
			Flags []struct {
				ID string `json:"id"`
			} `json:"flags"`
		}
		if err := json.Unmarshal(response.body, &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Flags) != 1 || body.Flags[0].ID != want {
			t.Fatalf("list ID does not match %s", want)
		}
		// The additive field is response-only; old request validation stays strict.
		requireError(t, a.request(t, "POST", collection, `{"key":"injected","id":"other/flag"}`, nil), 400, "invalid_request")
		requireError(t, a.request(t, "GET", collection+"/injected", "", nil), 404, "not_found")
		requireError(t, a.request(t, "PATCH", collection+"/checkout", `{"id":"other/flag"}`, nil), 400, "invalid_request")
		assertID(a.request(t, "GET", collection+"/checkout", "", nil), 200)
	}
}
