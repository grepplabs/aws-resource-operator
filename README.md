# AWS Resource Operator

**Note: This is an early alpha stage.**

AWS Resource Operator allows you to manage AWS resources using Kubernetes Custom Resource Definitions.

In contrast to the [aws-service-operator](https://github.com/awslabs/aws-service-operator), this operator uses AWS APIs directly without CloudFormation, just like the [terraform-provider-aws](https://github.com/terraform-providers/terraform-provider-aws) does.


## Operator reconciliations

### S3Bucket

- create and delete bucket
- [policy](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-bucket-policy.html)
- [canned ACL](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-bucket-acl.html)
    * reconciliation is based on object status
    * check [predefined set of grantees and permissions](https://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl)

