package app

import "testing"
import "github.com/c2stack/c2g/c2"

func Test_Exec(t *testing.T) {
	c2.DebugLog(true)
	e := &Exec{}
	a := &App{
		Id:   "c1",
		Type: "car",
		Startup: map[string]interface{}{
			"restconf": map[string]interface{}{
				"web": map[string]interface{}{
					"port": ":8090",
				},
				"debug": true,
				"callHome": map[string]interface{}{
					"deviceId":     "c1",
					"address":      "http://127.0.0.1:8080/restconf",
					"localAddress": "http://{REQUEST_ADDRESS}:8090/restconf",
				},
			},
		},
	}
	if err := e.NewApp(a); err != nil {
		t.Error(err)
	}
}
