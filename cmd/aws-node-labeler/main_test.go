package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	v1 "k8s.io/api/core/v1"
)

func TestInstanceIDFromProviderID(t *testing.T) {
	r, err := InstanceIDFromProviderID("aws:///us-east-1c/i-0393ac1bb853a1fb5")
	if err != nil {
		t.Fatal(err)
	}
	if r != "i-0393ac1bb853a1fb5" {
		t.Fatalf("expected i-0393ac1bb853a1fb5 got %s", r)
	}
}

func TestEC2TagIsManagableByTheLabeler(t *testing.T) {
	cases := []struct {
		input  string
		output bool
	}{
		{"hello", false},
		{"kubernetes/aws-labeler/label/type", true},
		{"kubernetes/aws-labeler/tail/type", true},
	}

	for _, v := range cases {
		t.Run("Input: "+v.input, func(t *testing.T) {
			if isManagable(v.input) != v.output {
				t.Errorf("different expectation for the instance tag %s", v.input)
			}
		})
	}
}

func TestSplitOfTheEC2TagKey(t *testing.T) {
	cases := []struct {
		input             string
		expectedOutputLen int
		output            bool
	}{
		{"hello", 0, false},
		{"kubernetes/aws-labeler", 0, false},
		{"kubernetes/aws-labeler/label/type", 4, true},
		{"kubernetes/aws-labeler/tail/type", 4, true},
		{"kubernetes/aws-labeler/what/ever", 4, true},
	}

	for _, v := range cases {
		t.Run("Input: "+v.input, func(t *testing.T) {
			res, err := splitEC2Tag(v.input)
			if err != nil && v.output {
				t.Error(err.Error())
			}
			if err == nil && len(res) != v.expectedOutputLen {
				t.Error("No error from the splitted function but the len is not what we expect")
			}
		})
	}
}

func TestGetTaintFromEC2Tag(t *testing.T) {
	cases := []struct {
		input  ec2.Tag
		output v1.Taint
	}{
		{
			ec2.Tag{
				Key:   aws.String("kubernetes/aws-labeler/tail/type"),
				Value: aws.String("ci:NoSchedule"),
			},
			v1.Taint{
				Key:    "awslabeler.com/type",
				Value:  "ci",
				Effect: v1.TaintEffectNoSchedule,
			},
		},
		{
			ec2.Tag{
				Key:   aws.String("kubernetes/aws-labeler/tail/type"),
				Value: aws.String("frontline:blabla"),
			},
			v1.Taint{
				Key:    "awslabeler.com/type",
				Value:  "frontline",
				Effect: v1.TaintEffectNoSchedule,
			},
		},
	}

	for _, v := range cases {
		t.Run("Input: "+*v.input.Key+"="+*v.input.Value, func(t *testing.T) {
			res, err := splitEC2Tag(*v.input.Key)
			if err != nil {
				t.Fatal(err)
			}
			taint, err := getTaintFromAWSTag(res, *v.input.Value)
			if err != nil {
				t.Fatal(err)
			}
			if taint.Value != v.output.Value {
				t.Errorf("Expected taint value %s got %s", v.output.Value, taint.Value)
			}
			if taint.Value != v.output.Value {
				t.Errorf("Expected taint effect %s got %s", v.output.Effect, taint.Effect)
			}
			if taint.Key != v.output.Key {
				t.Errorf("Expected taint effect %s got %s", v.output.Key, taint.Key)
			}
		})
	}
}
