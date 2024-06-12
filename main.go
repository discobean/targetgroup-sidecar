package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/namsral/flag"
	log "github.com/sirupsen/logrus"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var (
	instanceId         string
	targetGroupIds     string
	spotTerminationUrl = "http://169.254.169.254/latest/meta-data/spot/termination-time"

	gracefulStop = make(chan os.Signal)
	sess         = session.Must(session.NewSession())
)

func configureFromFlags() {
	flag.StringVar(&instanceId, "instanceid", "metadata", "instance id to use, or use metadata")
	flag.StringVar(&targetGroupIds, "targetgroupids", "", "comma separated list of target group ids")
	flag.Parse()

	if instanceId == "metadata" {
		log.Infof("Fetching Instance ID from EC2 metadata")
		metadata := ec2metadata.New(sess)
		result, err := metadata.GetMetadata("instance-id")
		if err != nil {
			log.Fatalf("Failed to fetch instance id: %v", err)
		}
		instanceId = result
	}
}

func dumpConfig() {
	log.Infof("INSTANCEID=%v\n", instanceId)
	log.Infof("TARGETGROUPIDS=%v\n", targetGroupIds)
}

func catchSignals() {
	sig := <-gracefulStop
	log.Infof("Caught Signal: %+v", sig)

	tearDownTargetGroups()
}

func tearDownTargetGroups() {
	svc := elbv2.New(sess)

	for _, targetGroupId := range strings.Split(targetGroupIds, ",") {
		log.Infof("Removing instance from target group %s => %s", instanceId, targetGroupId)

		input := &elbv2.DeregisterTargetsInput{
			TargetGroupArn: aws.String(targetGroupId),
			Targets: []*elbv2.TargetDescription{
				{
					Id: aws.String(instanceId),
				},
			},
		}

		_, err := svc.DeregisterTargets(input)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case elbv2.ErrCodeTargetGroupNotFoundException:
					log.Error(elbv2.ErrCodeTargetGroupNotFoundException, aerr.Error())
				case elbv2.ErrCodeTooManyTargetsException:
					log.Error(elbv2.ErrCodeTooManyTargetsException, aerr.Error())
				case elbv2.ErrCodeInvalidTargetException:
					log.Error(elbv2.ErrCodeInvalidTargetException, aerr.Error())
				case elbv2.ErrCodeTooManyRegistrationsForTargetIdException:
					log.Error(elbv2.ErrCodeTooManyRegistrationsForTargetIdException, aerr.Error())
				default:
					log.Error(aerr.Error())
				}
			} else {
				// Print the error, cast err to awserr.Error to get the Code and
				// Message from an error.
				log.Error(err.Error())
			}

			continue
		}

		log.Printf("Successfuly deregistered instance")
	}

	log.Printf("Deregistered instance from all targetgroups")
	log.Exit(0)
}

func setupTargetGroups() {
	svc := elbv2.New(sess)

	for _, targetGroupId := range strings.Split(targetGroupIds, ",") {
		log.Infof("Setting up instance in target group %s => %s", instanceId, targetGroupId)

		input := &elbv2.RegisterTargetsInput{
			TargetGroupArn: aws.String(targetGroupId),
			Targets: []*elbv2.TargetDescription{
				{
					Id: aws.String(instanceId),
				},
			},
		}

		select {
		case sig := <-gracefulStop:
			tearDownTargetGroups()
			log.Fatalf("Caught terminate signal during setup of target groups: %+v", sig)
		default:
		}

		_, err := svc.RegisterTargets(input)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case elbv2.ErrCodeTargetGroupNotFoundException:
					log.Error(elbv2.ErrCodeTargetGroupNotFoundException, aerr.Error())
				case elbv2.ErrCodeTooManyTargetsException:
					log.Error(elbv2.ErrCodeTooManyTargetsException, aerr.Error())
				case elbv2.ErrCodeInvalidTargetException:
					log.Error(elbv2.ErrCodeInvalidTargetException, aerr.Error())
				case elbv2.ErrCodeTooManyRegistrationsForTargetIdException:
					log.Error(elbv2.ErrCodeTooManyRegistrationsForTargetIdException, aerr.Error())
				default:
					log.Error(aerr.Error())
				}
			} else {
				// Print the error, cast err to awserr.Error to get the Code and
				// Message from an error.
				log.Error(err.Error())
			}

			// Deregister all the instances and quit
			tearDownTargetGroups()
			log.Fatal("Failed to register instance to all targetgroups, so deregistered and quit")
		}

		log.Print("Registered instance")
	}

	log.Print("Registered all instances onto the targetgroups")
}

func main() {
	configureFromFlags()
	dumpConfig()

	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT)

	setupTargetGroups()
	catchSignals()
}
