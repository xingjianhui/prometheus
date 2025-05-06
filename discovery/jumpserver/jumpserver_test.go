//// Copyright 2016 The Prometheus Authors
//// Licensed under the Apache License, Version 2.0 (the "License");
//// you may not use this file except in compliance with the License.
//// You may obtain a copy of the License at
////
////     http://www.apache.org/licenses/LICENSE-2.0
////
//// Unless required by applicable law or agreed to in writing, software
//// distributed under the License is distributed on an "AS IS" BASIS,
//// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//// See the License for the specific language governing permissions and
//// limitations under the License.
//
package jumpserver

import (
	"context"
	"fmt"
	"go.uber.org/goleak"
	"testing"
)


func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestJumpserver(t *testing.T) {
	fts := Filters{}
	fts.MatchGroups = append(fts.MatchGroups, Filter{
		Name: "ucloud",
		Value: "/Root/Infra/ucloud/",
	})
	fts.MismatchGroups = append(fts.MismatchGroups, Filter{
		Name:  "SRE",
		Value: "/Root/Infra/ucloud/SRE",
	})
	fts.MatchLabels = append(fts.MatchLabels, Filter{
		Name:  "Prometheus",
		Value: "True",
	})
	fts.MismatchLabels = append(fts.MismatchLabels, Filter{
		Name:  "Prometheus",
		Value: "False",
	})

	sdCondfig := &SDConfig{
		Server:          "https://jump.growingio.cn",
		Authorization:	       	"token a85f0ad845d1cded6a8d354bb4ea968b257604ef",
		Filters: 	fts,
		Port:            9000,
		RefreshInterval: 60,
	}
	sdCondfig.Name()
	d := NewDiscovery(sdCondfig, nil)
	ctx, _ := context.WithCancel(context.Background())
	//d.refreshJump()
	a, _ := d.refresh(ctx)
	fmt.Println(a)
}
