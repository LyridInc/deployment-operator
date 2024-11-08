/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1alpha1 "github.com/azhry/lyrid-operator/api/v1alpha1"
	"github.com/azhry/lyrid-operator/pkg/lyra"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppDeploymentReconciler reconciles a AppDeployment object
type AppDeploymentReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	k8sClient *kubernetes.Clientset
}

//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appdeployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *AppDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	accountSecret := &corev1.Secret{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: req.Namespace}, accountSecret); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	appDeploy := &appsv1alpha1.AppDeployment{}
	if err := r.Get(ctx, req.NamespacedName, appDeploy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppDeployment is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	isCreateNewRevision, _ := handleAppDeploymentChanges(ctx, r, *appDeploy)
	if isCreateNewRevision {
		appDeploy.Spec.CurrentRevisionId = "" // empty the revision id so that it will trigger new revision creation
	}

	lyraClient := lyra.NewLyraClient(r.Client, req.Namespace)
	syncAppResponse, err := lyraClient.SyncApp(*appDeploy, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
	if err != nil {
		return ctrl.Result{}, err
	}

	if syncAppResponse.ModuleRevision.ID != "" {
		handleRevisionChanges(ctx, r, *appDeploy, *syncAppResponse)
	}

	if isCreateNewRevision {
		// docker
		// deploymentEndpoint := req.Name + "." + req.Namespace + ".svc.cluster.local"
		service := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      appDeploy.Name,
				Namespace: appDeploy.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Protocol:   corev1.ProtocolTCP,
						Port:       80,
						TargetPort: intstr.FromInt(80),
					},
				},
				Selector: map[string]string{
					"app": syncAppResponse.FunctionCode.ID,
				},
				Type: "ClusterIP",
			},
		}

		r.k8sClient.CoreV1().Services(appDeploy.Namespace).Create(ctx, service, metav1.CreateOptions{})
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppDeployment{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				// Custom logic before creating an AppDeployment
				fmt.Println("create event")
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Custom logic before updating an AppDeployment
				fmt.Println("update event")
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				// Custom logic before deleting an AppDeployment
				fmt.Println("delete event")
				return true
			},
		}).
		Complete(r)
}

func handleAppDeploymentChanges(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment) (bool, error) {
	container := corev1.Container{
		Name:      appDeploy.Name,
		Image:     appDeploy.Spec.Image,
		Resources: appDeploy.Spec.Resources,
		Ports:     appDeploy.Spec.Ports,
	}

	if len(appDeploy.Spec.VolumeMounts) > 0 {
		container.VolumeMounts = appDeploy.Spec.VolumeMounts
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appDeploy.Name,
			Namespace: appDeploy.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &appDeploy.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": appDeploy.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appDeploy.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						container,
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(&appDeploy, dep, r.Scheme); err != nil {
		return false, err
	}

	var createNewRevision bool
	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, found); err != nil {
		// If the Deployment doesn't exist, create it
		err = r.Create(ctx, dep)
		if err != nil {
			return createNewRevision, err
		}
		createNewRevision = true
	} else {
		// Update the existing Deployment if necessary
		if *found.Spec.Replicas != appDeploy.Spec.Replicas || found.Spec.Template.Spec.Containers[0].Image != appDeploy.Spec.Image {
			found.Spec.Replicas = &appDeploy.Spec.Replicas
			found.Spec.Template.Spec.Containers[0].Image = appDeploy.Spec.Image

			// TODO: also do changes for another fields

			err = r.Update(ctx, found)
			if err != nil {
				return createNewRevision, err
			}

			// TODO: empty the revision so that whenever a change occured, it will create a new revision
			createNewRevision = true
		}

	}
	return createNewRevision, nil
}

func handleRevisionChanges(ctx context.Context, r *AppDeploymentReconciler, appDeploy appsv1alpha1.AppDeployment, syncApp lyrmodel.SyncAppResponse) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	fmt.Println("Current:", appDeploy.Spec.CurrentRevisionId)
	fmt.Println("New:", syncApp.ModuleRevision.ID)

	if appDeploy.Spec.CurrentRevisionId != syncApp.ModuleRevision.ID {
		appDeploy.Spec.CurrentRevisionId = syncApp.ModuleRevision.ID
		if err := r.Update(ctx, &appDeploy); err != nil {
			log.Error(err, "Failed to update current revision id on app deployment")
			return ctrl.Result{}, err
		}

		revisionsList := appsv1alpha1.RevisionList{}

		if err := r.Client.List(ctx, &revisionsList, client.InNamespace(appDeploy.GetNamespace()), client.MatchingFields{
			".metadata.ownerReferences": string(appDeploy.GetUID()),
		}); err != nil {
			fmt.Println(fmt.Errorf("failed to list revisions: %w", err))
		}

		for _, revision := range revisionsList.Items {
			revision.Status.Phase = "Non-active"

			if err := r.Client.Status().Update(ctx, &revision); err != nil {
				fmt.Println(fmt.Errorf("failed to update status for revision %s: %w", revision.Name, err))
			}
		}

		var spec = appDeploy.Spec
		newRevision := &appsv1alpha1.Revision{
			Spec: appsv1alpha1.RevisionSpec{
				Image:        spec.Image,
				ModuleId:     syncApp.Module.ID,
				Ports:        spec.Ports,
				Replicas:     spec.Replicas,
				Resources:    spec.Resources,
				VolumeMounts: spec.VolumeMounts,
			},
		}
		newRevision.SetName(syncApp.ModuleRevision.Title)
		newRevision.SetNamespace(appDeploy.Namespace)

		if err := controllerutil.SetControllerReference(
			&appDeploy,
			newRevision,
			r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Create the instance if it does not exist
		if err := r.Client.Create(ctx, newRevision); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to create new Revision instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to create new Revision instance")
			return ctrl.Result{}, err
		}

		newRevision.Status.Phase = "Active"
		newRevision.Status.Message = "Revision is up and running"
		newRevision.Status.Conditions = []metav1.Condition{
			{
				LastTransitionTime: metav1.Now(),
				Message:            newRevision.Status.Message,
				Type:               newRevision.Status.Phase,
				Status:             "true",
			},
		}
		if err := r.Status().Update(ctx, newRevision); err != nil {
			log.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
