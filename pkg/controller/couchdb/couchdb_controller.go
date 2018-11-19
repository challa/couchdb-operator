package couchdb

import (
	"context"
	"log"
	"reflect"

	cachev1alpha1 "github.com/operator-framework/operator-sdk-samples/couchdb-operator/pkg/apis/cache/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)


// Add creates a new Couchdb Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileCouchdb{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("couchdb-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Couchdb
	err = c.Watch(&source.Kind{Type: &cachev1alpha1.Couchdb{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Couchdb
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &cachev1alpha1.Couchdb{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileCouchdb{}

// ReconcileCouchdb reconciles a Couchdb object
type ReconcileCouchdb struct {
	// TODO: Clarify the split client
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Couchdb object and makes changes based on the state read
// and what is in the Couchdb.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Couchdb Deployment for each Couchdb CR
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCouchdb) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log.Printf("Reconciling Couchdb %s/%s\n", request.Namespace, request.Name)

	// Fetch the Couchdb instance
	couchdb := &cachev1alpha1.Couchdb{}
	err := r.client.Get(context.TODO(), request.NamespacedName, couchdb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Printf("Couchdb %s/%s not found. Ignoring since object must be deleted\n", request.Namespace, request.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Printf("Failed to get Couchdb: %v", err)
		return reconcile.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: couchdb.Name, Namespace: couchdb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		dep := r.deploymentForCouchdb(couchdb)
		log.Printf("Creating a new Deployment %s/%s\n", dep.Namespace, dep.Name)
		err = r.client.Create(context.TODO(), dep)
		if err != nil {
			log.Printf("Failed to create new Deployment: %v\n", err)
			return reconcile.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		log.Printf("Failed to get Deployment: %v\n", err)
		return reconcile.Result{}, err
	}

	// Ensure the deployment size is the same as the spec
	size := couchdb.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.client.Update(context.TODO(), found)
		if err != nil {
			log.Printf("Failed to update Deployment: %v\n", err)
			return reconcile.Result{}, err
		}
		// Spec updated - return and requeue
		return reconcile.Result{Requeue: true}, nil
	}

	// Update the Couchdb status with the pod names
	// List the pods for this couchdb's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForCouchdb(couchdb.Name))
	listOps := &client.ListOptions{Namespace: couchdb.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), listOps, podList)
	if err != nil {
		log.Printf("Failed to list pods: %v", err)
		return reconcile.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, couchdb.Status.Nodes) {
		couchdb.Status.Nodes = podNames
		err := r.client.Update(context.TODO(), couchdb)
		if err != nil {
			log.Printf("failed to update couchdb status: %v", err)
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// deploymentForCouchdb returns a Couchdb Deployment object
func (r *ReconcileCouchdb) deploymentForCouchdb(m *cachev1alpha1.Couchdb) *appsv1.Deployment {
	ls := labelsForCouchdb(m.Name)
	replicas := m.Spec.Size

	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:   "couchdb:2.2",
						Name:    "couchdb",
						//Command: []string{"couchdb", "-m=64", "-o", "modern", "-v"},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 5984,
							Name:          "couchdb",
						}},
					}},
				},
			},
		},
	}
	// Set Couchdb instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep
}

// labelsForMemcached returns the labels for selecting the resources
// belonging to the given memcached CR name.
func labelsForCouchdb(name string) map[string]string {
	return map[string]string{"app": "couchdb", "couchdb_cr": name}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}
