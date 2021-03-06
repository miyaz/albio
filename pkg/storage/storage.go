package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/pkg/errors"
	"github.com/yuuki/albio/pkg/model"
)

// LoadBalancer represents LoadBalancer information on storage.
type LoadBalancer struct {
	Name string `json:"name"`
	Arn  string `json:"arn"`
	Type string `json:"type"` // "application" or "network"
}

// SaveLoadBalancers saves LoadBalancers as JSON.
func SaveLoadBalancers(w io.Writer, instanceID string, loadbalancers model.LoadBalancers) error {
	var lbs []LoadBalancer
	for _, lb := range loadbalancers {
		lbs = append(lbs, LoadBalancer{
			Name: lb.Name,
			Arn:  lb.Arn,
			Type: lb.Type,
		})
	}
	result := struct {
		LoadBalancers []LoadBalancer `json:"loadbalancers"`
		InstanceID    string         `json:"instance-id"`
	}{
		LoadBalancers: lbs,
		InstanceID:    instanceID,
	}
	b, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(err, "json marshal error")
	}
	if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
		return errors.Wrap(err, "print json error")
	}
	return nil
}

// LoadLoadBalancers load LoadBalancers as JSON.
func LoadLoadBalancers(r io.Reader, instanceID string) ([]LoadBalancer, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return []LoadBalancer{}, errors.Wrap(err, "i/o read error")
	}
	var result struct {
		LoadBalancers []LoadBalancer `json:"loadbalancers"`
		InstanceID    string         `json:"instance-id"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return []LoadBalancer{}, errors.Wrap(err, "json unmarshal error")
	}
	if instanceID == result.InstanceID {
		return result.LoadBalancers, nil
	}
	return []LoadBalancer{}, nil
}
