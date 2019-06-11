# targetgroup-sidecar
Adds an instance to a targetgroup for NLB or ALB load balancing for instances.

Particularly useful if using DAEMON tasks in AWS ECS

1. Takes the instance ID from ec2 metadata (or `INSTANCEID` environment)
2. Adds the instance ID to an instance target group(s) specified by TARGETGROUPIDS (comma separated)
3. When SIGHUP happens, it removes instance from the load balancer
4. Then exits 0

Environment variables:
* `INSTANCEID` The instance ID (or set as metadata to auto-detect)
* `TARGETGROUPIDS` Comma separated target group IDs to register to

Build your own docker image:
```
make docker
```

Policies required for AWS ECS Role:
```
TODO
```
