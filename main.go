package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/namsral/flag"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	instanceId             string
	targetGroupIds         string
	monitorSpotTermination bool
	spotTerminationUrl     = "http://169.254.169.254/latest/meta-data/spot/termination-time"

	startTearDownTargetGroups = make(chan os.Signal)
	stopTerminationMonitoring = make(chan os.Signal)
	sess                      = session.Must(session.NewSession())
)

func configureFromFlags() {
	flag.StringVar(&instanceId, "instanceid", "metadata", "instance id to use, or use metadata")
	flag.StringVar(&targetGroupIds, "targetgroupids", "", "comma separated list of target group ids")
	flag.BoolVar(&monitorSpotTermination, "monitorspot", false, "Monitor Spot Termination and remove targetgroups")
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
	log.Infof("MONITORSPOT=%v", monitorSpotTermination)
}

func catchSignals() {
	sig := <-startTearDownTargetGroups
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
		case sig := <-startTearDownTargetGroups:
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

func monitorSpotTerminationSignal() {
	log.Info("Monitoring Spot Termination Signal")
	log.Info("Note - Container MetadataOptions.HttpTokens must be optional to view termination notice")

	sess := session.Must(session.NewSession())
	metadataSvc := ec2metadata.New(sess)

	if !metadataSvc.Available() {
		log.Warn("EC2 Metadata service is not available")
		return
	}

	// Check every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopTerminationMonitoring:
			log.Info("Stopping monitoring for spot termination signal")
			return

		case <-ticker.C:
			resp, err := http.Get(spotTerminationUrl)
			if err != nil {
				log.Error("Error checking termination notice: ", err)
				continue
			}

			if resp.StatusCode == 200 {
				log.Info("Spot instance termination notice received")
				log.Info("Sending signal to start teardown of target groups")
				startTearDownTargetGroups <- syscall.SIGTERM
				return
			}

			log.Debug("No termination notice, continuing monitoring")
		}
	}
}

func main() {
	configureFromFlags()
	dumpConfig()

	var wg sync.WaitGroup // Declare a WaitGroup

	if monitorSpotTermination {
		signal.Notify(stopTerminationMonitoring, syscall.SIGTERM)
		signal.Notify(stopTerminationMonitoring, syscall.SIGINT)

		wg.Add(1)
		go func() {
			defer wg.Done()
			monitorSpotTerminationSignal()
		}()
	}

	signal.Notify(startTearDownTargetGroups, syscall.SIGTERM)
	signal.Notify(startTearDownTargetGroups, syscall.SIGINT)

	wg.Add(1)
	go func() {
		defer wg.Done()
		setupTargetGroups()
		catchSignals()
	}()

	wg.Wait()
}
