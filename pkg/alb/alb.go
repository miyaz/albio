package alb

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/yuuki/albio/pkg/awsapi"
	"github.com/yuuki/albio/pkg/model"
)

type ALB interface {
	GetLoadBalancersFromInstanceID(string) (model.LoadBalancers, error)
}

type _alb struct {
	svc awsapi.ALBAPI
}

func New(sess *session.Session) ALB {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region, _ = ec2metadata.New(sess).Region()
	}
	return &_alb{
		svc: elbv2.New(sess, aws.NewConfig().WithRegion(region)),
	}
}

func (a *_alb) GetLoadBalancersFromInstanceID(instanceID string) (model.LoadBalancers, error) {
	groupResp, err := a.svc.DescribeTargetGroups(&elbv2.DescribeTargetGroupsInput{})
	if err != nil {
		return nil, err
	}

	loadBalancerArnToTargets := make(map[string][]*elbv2.TargetDescription)
	var loadBalancerArns []*string
	// TODO make API call concurrent
	for _, group := range groupResp.TargetGroups {
		resp, err := a.svc.DescribeTargetHealth(&elbv2.DescribeTargetHealthInput{
			TargetGroupArn: group.TargetGroupArn,
		})
		if err != nil {
			return nil, err
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
	loadBalancers, err := a.svc.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{
		LoadBalancerArns: loadBalancerArns,
	})
	if err != nil {
		return nil, err
	}

	return model.NewLoadBalancersFromALB(loadBalancers, loadBalancerArnToTargets), nil
}