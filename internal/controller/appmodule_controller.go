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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appsv1alpha1 "github.com/LyridInc/lyrid-operator/api/v1alpha1"
)

var operatorStartTime = time.Now()

// AppModuleReconciler reconciles a AppModule object
type AppModuleReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	K8sClient *kubernetes.Clientset
}

//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.lyrid.io,resources=appmodules/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppModule object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *AppModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// TODO(user): your logic here
	fmt.Println("------------------------------------- RECONCILE MODULE HERE -------------------------------------")

	accountSecret := &corev1.Secret{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: "lyrid.secretkey", Namespace: req.Namespace}, accountSecret); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("lyrid.secretkey secret is not found in namespace %s\n", req.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	fmt.Println("Request:", req.Name, req.Namespace)

	appModule := &appsv1alpha1.AppModule{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, appModule); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppModule is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if appModule.Generation != appModule.Status.ObservedGeneration {
		defer func() {
			appModule.Status.ObservedGeneration = appModule.Generation
			if err := r.Client.Status().Update(ctx, appModule); err != nil {
				fmt.Println("Update generation error:", err)
			}
		}()
	} else {
		return ctrl.Result{}, nil
	}

	if origin, ok := appModule.Annotations["origin"]; ok && origin == "app-deployment" {
		appModule.Annotations["origin"] = ""
		if err := r.Client.Update(ctx, appModule); err != nil {
			if !errors.IsAlreadyExists(err) {
				log.Error(err, "Failed to update Module instance")
				return ctrl.Result{}, err
			}
			log.Error(err, "Failed to update Module instance")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	appDeploy := &appsv1alpha1.AppDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: appModule.Spec.Ref.AppDeployment["name"], Namespace: req.Namespace}, appDeploy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AppDeployment is deleted ->")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	fmt.Println("app module: ", appModule.Spec.Id)
	fmt.Println("app deployment: ", appDeploy.Name)

	// lyraClient := lyra.NewLyraClient(r.Client, req.Namespace)
	// lyraClient.SyncModule(*appDeploy, string(accountSecret.Data["key"]), string(accountSecret.Data["secret"]))

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.AppModule{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
