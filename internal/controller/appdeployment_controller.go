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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1alpha1 "github.com/azhry/lyrid-operator/api/v1alpha1"
	"github.com/azhry/lyrid-operator/pkg/helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppDeploymentReconciler reconciles a AppDeployment object
type AppDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	resp, err := helpers.Authenticate(string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))
	if err != nil {
		return ctrl.Result{}, err
	}

	fmt.Println(resp.Token)

	appDeploy := &appsv1alpha1.AppDeployment{}
	if err := r.Get(ctx, req.NamespacedName, appDeploy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppDeployment is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

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

	if err := controllerutil.SetControllerReference(appDeploy, dep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, found); err != nil {
		// If the Deployment doesn't exist, create it
		err = r.Create(ctx, dep)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Update the existing Deployment if necessary
		if *found.Spec.Replicas != appDeploy.Spec.Replicas || found.Spec.Template.Spec.Containers[0].Image != appDeploy.Spec.Image {
			found.Spec.Replicas = &appDeploy.Spec.Replicas
			found.Spec.Template.Spec.Containers[0].Image = appDeploy.Spec.Image
			err = r.Update(ctx, found)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		// TODO: also do changes for another fields
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
