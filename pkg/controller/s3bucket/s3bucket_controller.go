/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package s3bucket

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	awsv1beta1 "github.com/grepplabs/aws-resource-operator/pkg/apis/aws/v1beta1"
	awsclient "github.com/grepplabs/aws-resource-operator/pkg/aws"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("s3bucket-controller")

// extra pattern from s3err/error.go
var s3ErrorPattern = regexp.MustCompile(`(?m:status code: .*, request id: .*, host id: .*)`)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new S3Bucket Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileS3Bucket{
		Client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		awsClient: awsclient.GetAWSClient(),
		recorder:  mgr.GetRecorder("s3bucket-controller"),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("s3bucket-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to S3Bucket
	err = c.Watch(&source.Kind{Type: &awsv1beta1.S3Bucket{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileS3Bucket{}

// ReconcileS3Bucket reconciles a S3Bucket object
type ReconcileS3Bucket struct {
	client.Client
	scheme    *runtime.Scheme
	awsClient *awsclient.AWSClient
	recorder  record.EventRecorder
}

// Reconcile reads that state of the cluster for a S3Bucket object and makes changes based on the state read
// and what is in the S3Bucket.Spec
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aws.grepplabs.com,resources=s3buckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aws.grepplabs.com,resources=s3buckets/status,verbs=get;update;patch
func (r *ReconcileS3Bucket) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the S3Bucket instance
	instance := &awsv1beta1.S3Bucket{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object.
		if !containsString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName)
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
	} else {
		// The object is being deleted
		if containsString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName) {
			// our finalizer is present, so lets handle our external dependency
			if err := r.deleteBucket(instance); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return reconcile.Result{}, err
			}

			// remove our finalizer from the list and update it.
			instance.ObjectMeta.Finalizers = removeString(instance.ObjectMeta.Finalizers, deleteBucketFinalizerName)
			if err := r.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
		// Our finalizer has finished, so the reconciler can do nothing.
		return reconcile.Result{}, nil
	}

	// Validate parameters before reconcile
	err = r.validateInstance(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	retryError := r.reconcileInstance(request, instance)
	if retryError != nil {
		if retryError.Reason != "" {
			r.sendEvent(instance, apiv1.EventTypeWarning, retryError.Reason, eventMessageFromError(retryError.Err))
		}
		return reconcile.Result{Requeue: retryError.Retryable}, retryError.Err
	}
	return reconcile.Result{}, nil
}

func eventMessageFromError(err error) string {
	errorString := fmt.Sprintf("%s", err)
	loc := s3ErrorPattern.FindStringIndex(errorString)
	var msg string
	if loc != nil {
		msg = strings.TrimSuffix(strings.TrimSpace(errorString[:loc[0]]), `\n\t`)
	}
	if msg != "" {
		return msg
	}
	return "Reconcile failed"
}

func (r *ReconcileS3Bucket) validateInstance(instance *awsv1beta1.S3Bucket) error {
	bucket := instance.Spec.Bucket

	if bucket == "" {
		return fmt.Errorf("Spec.Bucket is empty")
	}
	if instance.Status.ARN != "" {
		bucketARN := r.bucketARN(instance)
		if instance.Status.ARN != bucketARN {
			return fmt.Errorf("Spec.Bucket cannot be changed. ARNs: %s , %s", instance.Status.ARN, bucketARN)
		}
	}
	//TODO: region cannot be changed (WARNING only)
	return nil
}

func (r *ReconcileS3Bucket) bucketARN(instance *awsv1beta1.S3Bucket) string {
	return arn.ARN{
		Partition: r.awsClient.Partition(),
		Service:   "s3",
		Resource:  instance.Spec.Bucket,
	}.String()
}

func (r *ReconcileS3Bucket) bucketRegion(instance *awsv1beta1.S3Bucket) string {
	if instance.Spec.Region != "" {
		return instance.Spec.Region
	}
	return r.awsClient.Region()
}

func (r *ReconcileS3Bucket) reconcileInstance(request reconcile.Request, instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	log.Info("Reconcile S3 bucket", "bucket", instance.Spec.Bucket, "name", instance.Name)

	currentSpec, err := r.readBucket(instance)
	if err != nil {
		return err
	}
	if currentSpec == nil {
		currentSpec, err = r.createBucket(instance)
		if err != nil {
			return err
		}
	}

	// check created by operator
	if instance.Status.ARN == "" {
		if strings.EqualFold(string(instance.Spec.OwnershipStrategy), string(awsv1beta1.AcquireOwnershipStrategy)) {
			if retryError := r.acquireBucketOwnership(instance); retryError != nil {
				return retryError
			}
		} else {
			log.Info("[WARN] Cannot reconcile not own S3 bucket", "ownershipStrategy", instance.Spec.OwnershipStrategy)
			return awsclient.NonRetryableError(fmt.Errorf("cannot reconcile not own S3 bucket, use ownershipStrategy '%s' to force bucket management", awsv1beta1.AcquireOwnershipStrategy), "BucketNotOwned")
		}
		//TODO: can manage not owned (acquire)?
		//TODO: if yes update Status.ARN and Location
	}
	return nil
}

func (r *ReconcileS3Bucket) acquireBucketOwnership(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	s3conn := r.awsClient.S3()
	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	locationConstraint := r.bucketRegion(instance)

	log.Info("Acquiring S3 bucket ownership", "bucket", bucket, "locationConstraint", locationConstraint)

	// get bucket location
	getBucketLocationOutput, err := s3conn.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return awsclient.RetryableError(err, "GetBucketLocationFailed")
		}
		return awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket location (%s): %s", bucket, err), "GetBucketLocationFailed")
	}

	// update status
	log.Info("Updating status of acquired S3 bucket", "bucket", bucket)
	if getBucketLocationOutput.LocationConstraint != nil {
		if locationConstraint != *getBucketLocationOutput.LocationConstraint {
			errorString := fmt.Sprintf("location constraint configuration of S3Bucket changed from %s to %s", *getBucketLocationOutput.LocationConstraint, locationConstraint)
			log.Info("[ERROR] "+errorString, "bucket", instance.Spec.Bucket)
			return awsclient.NonRetryableError(fmt.Errorf(errorString), "AcquireBucketOwnershipFailed")
		}
		instance.Status.LocationConstraint = *getBucketLocationOutput.LocationConstraint
	}
	instance.Status.ARN = bucketArn

	if retryError := r.updateInstance(instance); retryError != nil {
		return retryError
	}
	r.sendEvent(instance, apiv1.EventTypeNormal, "BucketOwnershipAcquired", "S3 bucket %s", bucketArn)

	return nil
}

func (r *ReconcileS3Bucket) readBucket(instance *awsv1beta1.S3Bucket) (*awsv1beta1.S3BucketSpec, *awsclient.RetryError) {
	s3conn := r.awsClient.S3()
	bucket := instance.Spec.Bucket

	// check bucket exists
	_, err := s3conn.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{s3.ErrCodeNoSuchBucket}) {
			return nil, awsclient.RetryableError(err, "HeadBucketFailed")
		}
		if awsError, ok := err.(awserr.RequestFailure); ok && awsError.StatusCode() == 404 {
			log.Info("S3 Bucket not found", "bucket", bucket)
			return nil, nil
		}
		return nil, awsclient.NonRetryableError(fmt.Errorf("error reading S3 Bucket (%s): %s", bucket, err), "HeadBucketFailed")
	}
	result := &awsv1beta1.S3BucketSpec{
		Bucket: bucket,
	}

	// read policy
	/*
		policyOutput, err := s3conn.GetBucketPolicy(&s3.GetBucketPolicyInput{
			Bucket: aws.String(bucket),
		})
	*/

	return result, nil
}

func (r *ReconcileS3Bucket) createBucket(instance *awsv1beta1.S3Bucket) (*awsv1beta1.S3BucketSpec, *awsclient.RetryError) {
	s3conn := r.awsClient.S3()

	bucket := instance.Spec.Bucket
	bucketArn := r.bucketARN(instance)
	locationConstraint := r.bucketRegion(instance)

	// create bucket
	log.Info("Creating S3 bucket", "bucket", bucket, "locationConstraint", locationConstraint)
	_, err := s3conn.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		ACL:    aws.String(instance.Spec.Acl),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(locationConstraint),
		},
	})
	if err != nil {
		if awsclient.IsAWSCode(err, []string{"OperationAborted"}) {
			log.Error(err, "[WARN] Got an error while trying to create S3 bucket", "bucket", bucket)
			return nil, awsclient.RetryableError(fmt.Errorf("error creating S3 Bucket (%s): %s", bucket, err), "CreateBucketFailed")
		}
		return nil, awsclient.NonRetryableError(err, "CreateBucketFailed")
	}
	r.sendEvent(instance, apiv1.EventTypeNormal, "BucketCreated", "Successfully created S3 bucket %s", bucketArn)

	// update status
	log.Info("Updating status of new S3 bucket", "bucket", bucket)

	instance.Status.LocationConstraint = r.bucketRegion(instance)
	instance.Status.ARN = bucketArn

	if retryError := r.updateInstance(instance); retryError != nil {
		return nil, retryError
	}

	return &awsv1beta1.S3BucketSpec{
		Bucket: instance.Spec.Bucket,
		Region: instance.Spec.Region,
		Acl:    instance.Spec.Acl,
	}, nil
}

func (r *ReconcileS3Bucket) sendEvent(instance *awsv1beta1.S3Bucket, eventType, reason, messageFmt string, args ...interface{}) {
	r.recorder.Eventf(instance, eventType, reason, messageFmt, args...)
}

func (r *ReconcileS3Bucket) updateInstance(instance *awsv1beta1.S3Bucket) *awsclient.RetryError {
	if err := r.Update(context.Background(), instance); err != nil {
		log.Error(err, "[ERROR] Got an error while updating S3Bucket status", "bucket", instance.Spec.Bucket)
		return awsclient.NonRetryableError(err, "UpdateInstanceFailed")
	}
	return nil
}

func (r *ReconcileS3Bucket) deleteBucket(instance *awsv1beta1.S3Bucket) error {
	if strings.EqualFold(string(instance.Spec.DeleteStrategy), string(awsv1beta1.SkipDeleteStrategy)) {
		log.Info("Skip S3 bucket deletion. Delete strategy is Skip")
		return nil
	}
	bucketArn := instance.Status.ARN
	if bucketArn == "" {
		log.Info("Skip S3 bucket deletion. Status.ARN is empty", "bucket", instance.Spec.Bucket)
		return nil
	}
	parsedArn, err := arn.Parse(bucketArn)
	if err != nil {
		log.Error(err, "Skip S3 bucket deletion. Status.ARN is invalid", "bucket", instance.Spec.Bucket)
		return nil
	}
	bucket := parsedArn.Resource
	log.Info("Deleting S3 bucket", "bucket", bucket, "arn", instance.Status.ARN)

	s3conn := r.awsClient.S3()
	_, err = s3conn.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(parsedArn.Resource),
	},
	)
	if awsclient.IsAWSErr(err, s3.ErrCodeNoSuchBucket, "") {
		log.Info("[WARN] S3 Bucket to delete not found", "bucket", bucket)
		return nil
	}
	if awsclient.IsAWSErr(err, "BucketNotEmpty", "") {
		if strings.EqualFold(string(instance.Spec.DeleteStrategy), string(awsv1beta1.ForceDeleteStrategy)) {
			//TODO: delete objects from the bucket
		}

	}
	return err
}
