package elbv2

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/pkg/errors"
	"github.com/yuuki/albio/pkg/awsapi"
	"github.com/yuuki/albio/pkg/model"
	"golang.org/x/sync/errgroup"
)

type ELBv2 interface {
	GetLoadBalancersFromInstanceID(string) (model.LoadBalancers, error)
	GetLoadBalancersByNames([]string) (model.LoadBalancers, error)
	AddInstanceToLoadBalancers(string, model.LoadBalancers) error
	RemoveInstanceFromLoadBalancers(string, model.LoadBalancers) error
}

type _elbv2 struct {
	svc awsapi.ELBv2API
}

// New creates an ELBv2 client.
func New(sess *session.Session) ELBv2 {
	return &_elbv2{
		svc: elbv2.New(sess),
	}
}

func (a *_elbv2) GetLoadBalancersFromInstanceID(instanceID string) (model.LoadBalancers, error) {
	groupResp, err := a.svc.DescribeTargetGroups(&elbv2.DescribeTargetGroupsInput{})
	if err != nil {
		return nil, errors.Wrap(err, "describe target groups error")
	}

	loadBalancerArnToTargets := make(map[string][]*elbv2.TargetDescription)
	var loadBalancerArns []*string
	// TODO make API call concurrent
	for _, group := range groupResp.TargetGroups {
		resp, err := a.svc.DescribeTargetHealth(&elbv2.DescribeTargetHealthInput{
			TargetGroupArn: group.TargetGroupArn,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "describe target health error: %v", *group.TargetGroupArn)
		}

		found := false
		for _, desc := range resp.TargetHealthDescriptions {
			if *desc.Target.Id == instanceID {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		for _, desc := range resp.TargetHealthDescriptions {
			for _, arn := range group.LoadBalancerArns {
				loadBalancerArnToTargets[*arn] = append(
					loadBalancerArnToTargets[*arn], desc.Target,
				)
			}
		}
		loadBalancerArns = append(loadBalancerArns, group.LoadBalancerArns...)
	}
	resp, err := a.svc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
		LoadBalancerArns: loadBalancerArns,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "describe loadbalancers error: %v", loadBalancerArns)
	}

	return model.NewLoadBalancersFromELBv2(resp.LoadBalancers, loadBalancerArnToTargets), nil
}

// GetLoadBalancersByNames finds LoadBalancers by loadbalancer name.
func (a *_elbv2) GetLoadBalancersByNames(lbNames []string) (model.LoadBalancers, error) {
	loadbalancers, groups, err := a.findLoadBalancersAndTargetGroupsByLoadBalancerNames(lbNames)
	if err != nil {
		return nil, err
	}

	loadBalancerArnToTargets := make(map[string][]*elbv2.TargetDescription)
	// TODO make API call concurrent
	for _, group := range groups {
		resp, err := a.svc.DescribeTargetHealth(&elbv2.DescribeTargetHealthInput{
			TargetGroupArn: group.TargetGroupArn,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "describe target health: %v", group.TargetGroupArn)
		}
		for _, desc := range resp.TargetHealthDescriptions {
			for _, arn := range group.LoadBalancerArns {
				loadBalancerArnToTargets[*arn] = append(
					loadBalancerArnToTargets[*arn], desc.Target,
				)
			}
		}
	}

	return model.NewLoadBalancersFromELBv2(loadbalancers, loadBalancerArnToTargets), nil
}

func (a *_elbv2) AddInstanceToLoadBalancers(instanceID string, lbs model.LoadBalancers) error {
	_, groups, err := a.findLoadBalancersAndTargetGroupsByLoadBalancerNames(lbs.NameSlice())
	if err != nil {
		return err
	}

	eg := errgroup.Group{}
	for _, group := range groups {
		grp := group
		eg.Go(func() error {
			targets := []*elbv2.TargetDescription{{Id: aws.String(instanceID)}}
			_, err := a.svc.RegisterTargets(&elbv2.RegisterTargetsInput{
				TargetGroupArn: group.TargetGroupArn,
				Targets:        targets,
			})
			if err != nil {
				return errors.Wrapf(err, "register targets error: %v", group.TargetGroupArn)
			}
			return a.svc.WaitUntilTargetInService(&elbv2.DescribeTargetHealthInput{
				TargetGroupArn: grp.TargetGroupArn,
				Targets:        targets,
			})
		})
	}
	return eg.Wait()
}

func (a *_elbv2) RemoveInstanceFromLoadBalancers(instanceID string, lbs model.LoadBalancers) error {
	_, groups, err := a.findLoadBalancersAndTargetGroupsByLoadBalancerNames(lbs.NameSlice())
	if err != nil {
		return err
	}

	eg := errgroup.Group{}
	for _, group := range groups {
		grp := group
		eg.Go(func() error {
			targets := []*elbv2.TargetDescription{{Id: aws.String(instanceID)}}
			_, err := a.svc.DeregisterTargets(&elbv2.DeregisterTargetsInput{
				TargetGroupArn: group.TargetGroupArn,
				Targets:        targets,
			})
			if err != nil {
				return errors.Wrapf(err, "deregister targets error: %v", group.TargetGroupArn)
			}
			return a.svc.WaitUntilTargetDeregistered(&elbv2.DescribeTargetHealthInput{
				TargetGroupArn: grp.TargetGroupArn,
				Targets:        targets,
			})
		})
	}
	return eg.Wait()
}

func (a *_elbv2) findLoadBalancersAndTargetGroupsByLoadBalancerNames(lbNames []string) ([]*elbv2.LoadBalancer, []*elbv2.TargetGroup, error) {
	names := make([]*string, 0, len(lbNames))
	for _, n := range lbNames {
		names = append(names, aws.String(n))
	}

	lbResp, err := a.svc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
		Names: names,
	})
	if err != nil {
		return nil, nil, errors.Wrapf(err, "describe loadbalancers error: %v", names)
	}
	groups := make([]*elbv2.TargetGroup, 0, len(lbResp.LoadBalancers))
	for _, lb := range lbResp.LoadBalancers {
		resp, err := a.svc.DescribeTargetGroups(&elbv2.DescribeTargetGroupsInput{
			LoadBalancerArn: lb.LoadBalancerArn,
		})
		if err != nil {
			return nil, nil, errors.Wrapf(err, "describe target groups: %v", lb.LoadBalancerArn)
		}
		groups = append(groups, resp.TargetGroups...)
	}
	return lbResp.LoadBalancers, groups, nil
}
