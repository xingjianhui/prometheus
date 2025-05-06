// Copyright 2020 The github/xingjianhui Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jumpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/prometheus/discovery/refresh"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"

	//"encoding/json"
	//"fmt"
	//"github.com/go-kit/kit/log"
	//"github.com/go-kit/kit/log/level"
	//"github.com/pkg/errors"
	//"github.com/prometheus/client_golang/prometheus"
	//"github.com/prometheus/common/config"
	//"github.com/prometheus/common/model"
	//"io/ioutil"
	//"os"
	//"path/filepath"
	//"regexp"
	//"strings"
	//"sync"
	//"time"

	//"github.com/aws/aws-sdk-go/aws/ec2metadata"
	//"github.com/aws/aws-sdk-go/aws/session"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	//"github.com/prometheus/client_golang/prometheus"
	//"github.com/prometheus/common/config"
	//"github.com/prometheus/common/model"
	//fsnotify "gopkg.in/fsnotify/fsnotify.v1"
	//yaml "gopkg.in/yaml.v2"
	"github.com/prometheus/common/model"

	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

const (
	jumpNodesUri 		= "/api/v1/assets/nodes/"
	jumpAssetsUri 		= "/api/v1/assets/assets/"
	jumpLabelUri 		= "/api/v1/assets/labels/"
	jumpLabel 			=	model.MetaLabelPrefix + "jumpserver_"
	jumpLabelPrivateIP	= jumpLabel + "private_ip"
	jumpLabelPublicIP	= jumpLabel + "public_ip"
	jumpLabelHostname	= jumpLabel + "hostname"
	jumpLabelActive		= jumpLabel + "active"
	jumpLabelTag		= jumpLabel + "tag_"
	jumpLabelGroup		= jumpLabel + "group_"
	jumpLabelLocation	= jumpLabel + "location_"
)

var DefaultSDConfig = SDConfig{
	Server:            "localhost:8080",
	Location:			"/",
	Port:				80,
	RefreshInterval: 	model.Duration(60 * time.Second),
}

func init() {
	discovery.RegisterConfig(&SDConfig{})
}

type Filter struct {
	Name 	string		`yaml:"name"`
	Value 	string		`yaml:"value"`
}

type Filters struct {
	MatchGroups			[]Filter	`yaml:"match_groups,omitempty"`
	MismatchGroups		[]Filter	`yaml:"mismatch_groups,omitempty"`
	MatchLabels			[]Filter	`yaml:"match_labels"`
	MismatchLabels		[]Filter	`yaml:"mismatch_labels"`
}

// SDConfig is the configuration for file based discovery.
type SDConfig struct {
	Server 				string			`yaml:"server"`
	Authorization 		string			`yaml:"authorization"`
	Location			string			`yaml:"location,omitempty"`
	Port				int				`yaml:"port"`
	Filters 			Filters			`yaml:"filters"`
	RefreshInterval model.Duration 		`yaml:"refresh_interval,omitempty"`
}

// Name returns the name of the Config.
func (*SDConfig) Name() string { return "jumpserver" }

// NewDiscoverer returns a Discoverer for the Config.
func (c *SDConfig) NewDiscoverer(opts discovery.DiscovererOptions) (discovery.Discoverer, error) {
	return NewDiscovery(c, opts.Logger), nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *SDConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultSDConfig
	type plain SDConfig
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Server) == "" {
		return errors.New("JumpServer SD configuration requires a server address")
	}
	return nil
}

// NewDiscovery returns a new file discovery for the given paths.
func NewDiscovery(conf *SDConfig, logger log.Logger) *Discovery {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	conf.Filters.MatchGroups = append(conf.Filters.MatchGroups, Filter{
		Name:  "ROOT",
		Value: conf.Location,
	})
	d := &Discovery{
		server: conf.Server,
		authorization: conf.Authorization,
		location: conf.Location,
		filters:	conf.Filters,
		nodesUri: jumpNodesUri,
		assetsUri: jumpAssetsUri,
		labelsUri: jumpLabelUri,
		port: conf.Port,
		refreshInterval: time.Duration(conf.RefreshInterval),
		logger: logger,
	}
	d.Discovery = refresh.NewDiscovery(
		logger,
		"jumpserver",
		time.Duration(conf.RefreshInterval),
		d.refresh,
		)
	return d
}

// Discovery provides service discovery functionality based
// on files that contain target groups in JSON or YAML format. Refreshing
// happens using file watches and periodic refreshes.
type Discovery struct {
	*refresh.Discovery
	server				string
	authorization 		string
	location 			string
	filters 			Filters
	nodesUri			string
	assetsUri			string
	labelsUri			string
	port				int
	assets				[]asset
	nodes 				map[string]node
	labels				map[string]label
	refreshInterval  	time.Duration
	logger      		log.Logger
}

type node struct {
	Id			string	`json:"id"`
	Key 		string	`json:"key,omitempty"`
	Value 		string	`json:"value,omitempty"`
	FullValue	string	`json:"full_value,omitempty"`
	OrgName		string	`json:"org_name,omitempty"`
}

type asset struct {
	Id 			string		`json:"id"`
	IP 			string		`json:"ip"`
	Hostname	string		`json:"hostname"`
	Active		bool		`json:"is_active,omitempty"`
	PublicIP 	string		`json:"public_ip,omitempty"`
	Comment 	string		`json:"comment,omitempty"`
	Nodes 		[]string	`json:"nodes,omitempty"`
	Labels		[]string	`json:"labels,omitempty"`
}

type label struct {
	Id 			string		`json:"id"`
	Name		string		`json:"name"`
	Value 		string		`json:"value"`
	Category	string		`json:"category"`
	IsActive 	bool		`json:"is_active"`
	Comment 	string		`json:"comment"`
	Assets 		[]string	`json:"assets"`
}

func (d *Discovery) refresh(ctx context.Context) ([]*targetgroup.Group, error) {
	d.refreshJump()
	tg := &targetgroup.Group{
		Source: d.location,
	}
	for _, asset := range d.assets {
		if asset.IP == "" {
			continue
		}

		groups := d.filter(asset.Nodes, asset.Labels)
		if len(groups) == 0 {
			continue
		}

		labels := model.LabelSet{
			jumpLabelPrivateIP: model.LabelValue(asset.IP),
		}

		addr := net.JoinHostPort(asset.IP, fmt.Sprintf("%d", d.port))
		labels[model.AddressLabel] = model.LabelValue(addr)

		if asset.PublicIP != "" {
			labels[jumpLabelPublicIP] =  model.LabelValue(asset.PublicIP)
		}

		if asset.Hostname != "" {
			labels[jumpLabelHostname] = model.LabelValue(asset.Hostname)
		}
		//if asset.Active  {
		labels[jumpLabelActive] = model.LabelValue(strconv.FormatBool(asset.Active))
		//}
		for _, tagId := range asset.Labels {
			labels[jumpLabelTag + model.LabelName(d.labels[tagId].Name)] = model.LabelValue(d.labels[tagId].Value)
		}
		for i, group := range groups {
			labels[jumpLabelGroup + model.LabelName(strconv.Itoa(i))] = model.LabelValue(group.Name)
			labels[jumpLabelLocation + model.LabelName(strconv.Itoa(i))] = model.LabelValue(group.Value)
		}
		tg.Targets = append(tg.Targets, labels)

	}
	return []*targetgroup.Group{tg}, nil
}

func (d *Discovery) filter(nodes, labels []string) []Filter {
	var lcs, mlcs []Filter
	isNext := true

	if len(d.filters.MatchLabels) != 0 {
		isNext = false
		for _, mlb := range d.filters.MatchLabels {
			for _, labelId := range labels {
				lb := Filter{}
				lb.Name = d.labels[labelId].Name
				lb.Value = d.labels[labelId].Value
				if mlb.Name == lb.Name && mlb.Value == lb.Value {
					isNext = true
				}
			}
		}
	}
	if len(d.filters.MismatchLabels) != 0 {
		for _, mlb := range d.filters.MismatchLabels {
			for _, labelId := range labels {
				lb := Filter{}
				lb.Name = d.labels[labelId].Name
				lb.Value = d.labels[labelId].Value
				if mlb.Name == lb.Name && mlb.Value == lb.Value {
					isNext = false
				}
			}
		}
	}

	if isNext == false {
		return lcs
	}

	for _, nodeId := range nodes {
		lc := Filter{}
		lc.Name = d.nodes[nodeId].Value
		lc.Value = d.nodes[nodeId].FullValue
		if len(d.filters.MatchGroups) != 0 {
			if includeLocation(d.filters.MatchGroups, lc.Value) == true {
				mlcs = append(mlcs, lc)
			}
		} else {
			mlcs = append(mlcs, lc)
		}
	}
	for _, lc := range mlcs {
		if len(d.filters.MismatchGroups) != 0 {
			if includeLocation(d.filters.MismatchGroups, lc.Value) == false {
				lcs = append(lcs, lc)
			}
		} else {
			lcs = append(lcs, lc)
		}
	}
	return lcs
}

func includeLocation(parents []Filter, children string) bool {
	for _, lc := range parents {
		if strings.HasSuffix(lc.Value, "/") == false {
			lc.Value = lc.Value + "/"
		}
		if strings.HasSuffix(children, "/") == false {
			children = children + "/"
		}
		if l := len(lc.Value); l <= len(children) && lc.Value == children[:l] {
			return true
		}
	}
	return false
}

func (d *Discovery) refreshJump() {
	if err := json.Unmarshal(d.requestJump(d.assetsUri), &d.assets); err != nil {
		level.Error(d.logger).Log(err)
	}
	var nodes []node
	d.nodes = make(map[string]node)
	if err := json.Unmarshal(d.requestJump(d.nodesUri), &nodes); err != nil {
		level.Error(d.logger).Log(err)
	}
	for _, nd := range nodes {
		nd.FullValue = formatLocation(nd.FullValue)
		d.nodes[nd.Id] = nd
	}
	var labels []label
	d.labels = make(map[string]label)
	if err := json.Unmarshal(d.requestJump(d.labelsUri), &labels); err != nil {
		level.Error(d.logger).Log(err)
	}
	for _, lb := range labels {
		d.labels[lb.Id] = lb
	}
}

func (d *Discovery) requestJump(uri string) []byte {
	u, err := url.Parse(d.server)
	if err != nil {
		level.Error(d.logger).Log(err)
	}
	u.Path = path.Join(u.Path, uri)
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		level.Error(d.logger).Log(err)
	}
	req.Header.Add("Authorization", d.authorization)
	req.Header.Add("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		level.Error(d.logger).Log("Error on response: ", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		level.Error(d.logger).Log("Error on response's body: ", err)
	}
	return body
}

func formatLocation(lc string) string {
	c := strings.Replace(lc, " / ", "/", -1)
	c = path.Join("/", c)
	return c
}