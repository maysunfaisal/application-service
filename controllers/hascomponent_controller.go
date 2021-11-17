/*
Copyright 2021 Red Hat, Inc.

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

package controllers

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	devfileAPIV1 "github.com/devfile/api/v2/pkg/apis/workspaces/v1alpha2"
	"github.com/devfile/api/v2/pkg/attributes"
	devfilePkg "github.com/devfile/library/pkg/devfile"
	"github.com/devfile/library/pkg/devfile/parser"
	"github.com/devfile/library/pkg/devfile/parser/data/v2/common"
	"github.com/go-git/go-git/v5"
	appstudiov1alpha1 "github.com/redhat-appstudio/application-service/api/v1alpha1"
	devfile "github.com/redhat-appstudio/application-service/pkg/devfile"
)

const (
	devfileName     = "devfile.yaml"
	clonePathPrefix = "/tmp/appstudio/has"
)

// HASComponentReconciler reconciles a HASComponent object
type HASComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=appstudio.redhat.com,resources=hascomponents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=appstudio.redhat.com,resources=hascomponents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=appstudio.redhat.com,resources=hascomponents/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HASComponent object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *HASComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// your logic here
	logger.Info("HELLO from the controller")

	// Fetch the HASComponent instance
	var hasComponent appstudiov1alpha1.HASComponent
	err := r.Get(ctx, req.NamespacedName, &hasComponent)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// If the devfile hasn't been populated, the CR was just created
	if hasComponent.Status.Devfile == "" {
		source := hasComponent.Spec.Source
		context := hasComponent.Spec.Context
		var devfilePath string

		// append context to devfile if present
		// context is usually set when the git repo is a multi-component repo (example - contains both frontend & backend)
		if context == "" {
			devfilePath = devfileName
		} else {
			devfilePath = path.Join(context, devfileName)
		}

		logger.Info("calculated devfile path", "devfilePath", devfilePath)

		if source.GitSource.URL != "" {
			var devfileBytes []byte

			if source.GitSource.DevfileURL == "" {
				logger.Info("source.GitSource.URL", "source.GitSource.URL", source.GitSource.URL)
				rawURL, err := convertGitHubURL(source.GitSource.URL)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("rawURL", "rawURL", rawURL)

				devfilePath = rawURL + "/" + devfilePath
				logger.Info("devfilePath", "devfilePath", devfilePath)
				resp, err := http.Get(devfilePath)
				if err != nil {
					return ctrl.Result{}, err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					logger.Info("curl succesful")
					devfileBytes, err = ioutil.ReadAll(resp.Body)
					if err != nil {
						return ctrl.Result{}, err
					}
				} else {
					logger.Info("intializing cloning since unable to curl")
					clonePath := path.Join(clonePathPrefix, hasComponent.Spec.Application, hasComponent.Spec.ComponentName)

					// Check if the clone path is empty, if not delete it
					isDirExist, err := IsExist(clonePath)
					if err != nil {
						return ctrl.Result{}, err
					}
					if isDirExist {
						logger.Info("clone path exists, deleting", "path", clonePath)
						os.RemoveAll(clonePath)
					}

					// Clone the repo
					_, err = git.PlainClone(clonePath, false, &git.CloneOptions{
						URL: source.GitSource.URL,
					})
					if err != nil {
						return ctrl.Result{}, err
					}

					// Read the devfile
					devfileBytes, err = ioutil.ReadFile(path.Join(clonePath, devfilePath))
					if err != nil {
						return ctrl.Result{}, err
					}
				}
			} else {
				logger.Info("Getting devfile from the DevfileURL", "DevfileURL", source.GitSource.DevfileURL)
				resp, err := http.Get(source.GitSource.DevfileURL)
				if err != nil {
					return ctrl.Result{}, err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					logger.Info("curl succesful")
					devfileBytes, err = ioutil.ReadAll(resp.Body)
					if err != nil {
						return ctrl.Result{}, err
					}
				} else {
					return ctrl.Result{}, fmt.Errorf("unable to GET from %s", source.GitSource.DevfileURL)
				}
			}

			logger.Info("successfully read the devfile", "string representation", string(devfileBytes[:]))

			devfileObj, _, err := devfilePkg.ParseDevfileAndValidate(parser.ParserArgs{
				Data: devfileBytes,
			})
			if err != nil {
				return ctrl.Result{}, err
			}

			components, err := devfileObj.Data.GetComponents(common.DevfileOptions{
				ComponentOptions: common.ComponentOptions{
					ComponentType: devfileAPIV1.ContainerComponentType,
				},
			})
			if err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("components", "components", components)

			for i, component := range components {
				updateRequired := false
				if hasComponent.Spec.Route != "" {
					logger.Info("hasComponent.Spec.Route", "hasComponent.Spec.Route", hasComponent.Spec.Route)
					if len(component.Attributes) == 0 {
						logger.Info("init Attributes 1")
						component.Attributes = attributes.Attributes{}
					}
					logger.Info("len(component.Attributes) 111", "len(component.Attributes) 111", len(component.Attributes))
					component.Attributes = component.Attributes.PutString("appstudio/has.route", hasComponent.Spec.Route)
					updateRequired = true
				}
				if hasComponent.Spec.Replicas > 0 {
					logger.Info("hasComponent.Spec.Replicas", "hasComponent.Spec.Replicas", hasComponent.Spec.Replicas)
					if len(component.Attributes) == 0 {
						logger.Info("init Attributes 2")
						component.Attributes = attributes.Attributes{}
					}
					logger.Info("len(component.Attributes) 222", "len(component.Attributes) 222", len(component.Attributes))
					component.Attributes = component.Attributes.PutInteger("appstudio/has.replicas", hasComponent.Spec.Replicas)
					updateRequired = true
				}
				if i == 0 && hasComponent.Spec.TargetPort > 0 {
					logger.Info("hasComponent.Spec.TargetPort", "hasComponent.Spec.TargetPort", hasComponent.Spec.TargetPort)
					for i, endpoint := range component.Container.Endpoints {
						logger.Info("foudn endpoint", "endpoing", endpoint.Name)
						endpoint.TargetPort = hasComponent.Spec.TargetPort
						updateRequired = true
						component.Container.Endpoints[i] = endpoint
					}
				}
				for _, env := range hasComponent.Spec.Env {
					if env.ValueFrom != nil {
						return ctrl.Result{}, fmt.Errorf("env.ValueFrom is not supported at the moment, use env.value")
					}

					name := env.Name
					value := env.Value
					isPresent := false

					for i, devfileEnv := range component.Container.Env {
						if devfileEnv.Name == name {
							isPresent = true
							devfileEnv.Value = value
							component.Container.Env[i] = devfileEnv
						}
					}

					if !isPresent {
						component.Container.Env = append(component.Container.Env, devfileAPIV1.EnvVar{Name: name, Value: value})
					}
					updateRequired = true
				}

				if updateRequired {
					logger.Info("UPDATING COMPONENT", "component name", component.Container)
					// Update the component once it has been updated with the HAS Component data
					err := devfileObj.Data.UpdateComponent(component)
					if err != nil {
						return ctrl.Result{}, err
					}
				}
			}

			logger.Info("outside before getting NEW CONTENT")

			// Get the HASApplication CR
			hasApplication := appstudiov1alpha1.HASApplication{}
			err = r.Get(ctx, types.NamespacedName{Name: hasComponent.Spec.Application, Namespace: hasComponent.Namespace}, &hasApplication)
			if err != nil {
				logger.Info("Failed to get Application")
				return ctrl.Result{}, nil
			}
			if hasApplication.Status.Devfile != "" {
				// Get the devfile of the hasApp CR
				hasAppDevfileData, err := devfile.ParseDevfileModel(hasApplication.Status.Devfile)
				if err != nil {
					logger.Info(fmt.Sprintf("Unable to parse devfile model, exiting reconcile loop %v", req.NamespacedName))
					return ctrl.Result{}, err
				}

				newProject := devfileAPIV1.Project{
					Name: hasComponent.Spec.ComponentName,
					ProjectSource: devfileAPIV1.ProjectSource{
						Git: &devfileAPIV1.GitProjectSource{
							GitLikeProjectSource: devfileAPIV1.GitLikeProjectSource{
								Remotes: map[string]string{
									"origin": hasComponent.Spec.Source.GitSource.URL,
								},
							},
						},
					},
				}
				projects, err := hasAppDevfileData.GetProjects(common.DevfileOptions{})
				if err != nil {
					logger.Info(fmt.Sprintf("Unable to get projects %v", req.NamespacedName))
					return ctrl.Result{}, err
				}
				for _, project := range projects {
					if project.Name == newProject.Name {
						return ctrl.Result{}, fmt.Errorf("HASApplication already has a project with name %s", newProject.Name)
					}
				}
				err = hasAppDevfileData.AddProjects([]devfileAPIV1.Project{newProject})
				if err != nil {
					logger.Info(fmt.Sprintf("Unable to add projects %v", req.NamespacedName))
					return ctrl.Result{}, err
				}

				yamlHASCompData, err := yaml.Marshal(devfileObj.Data)
				if err != nil {
					return ctrl.Result{}, err
				}

				logger.Info("successfully UPDATED the devfile", "string representation", string(yamlHASCompData[:]))

				hasComponent.Status.Devfile = string(yamlHASCompData[:])
				err = r.Status().Update(ctx, &hasComponent)
				if err != nil {
					return ctrl.Result{Requeue: true}, err
				}

				// Update the HASApp CR with the new devfile
				yamlHASAppData, err := yaml.Marshal(hasAppDevfileData)
				if err != nil {
					logger.Info(fmt.Sprintf("Unable to marshall HASApplication devfile, exiting reconcile loop %v", req.NamespacedName))
					return ctrl.Result{}, err
				}
				hasApplication.Status.Devfile = string(yamlHASAppData)
				err = r.Status().Update(ctx, &hasApplication)
				if err != nil {
					return ctrl.Result{Requeue: true}, err
				}

			} else {
				return ctrl.Result{}, fmt.Errorf("HASApplication devfile model is empty. Before creating a HASComponent, an instance of HASApplication should be created")
			}

		} else if hasComponent.Spec.Source.ImageSource.ContainerImage != "" {
			return ctrl.Result{}, fmt.Errorf("container image is not supported at the moment, please use github links for adding a component to an application")
		}
	} else {
		// If the model already exists, see if fields have been updated
		return ctrl.Result{}, fmt.Errorf("not yet implemented")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HASComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appstudiov1alpha1.HASComponent{}).
		Complete(r)
}

// IsExist returns whether the given file or directory exists
func IsExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// convertGitHubURL converts a git url to its raw format
// taken from Jingfu's odo code
func convertGitHubURL(URL string) (string, error) {
	url, err := url.Parse(URL)
	if err != nil {
		return "", err
	}

	if strings.Contains(url.Host, "github") && !strings.Contains(url.Host, "raw") {
		// Convert path part of the URL
		URLSlice := strings.Split(URL, "/")
		if len(URLSlice) > 2 && URLSlice[len(URLSlice)-2] == "tree" {
			// GitHub raw URL doesn't have "tree" structure in the URL, need to remove it
			URL = strings.Replace(URL, "/tree", "", 1)
		} else {
			// Add "main" branch for GitHub raw URL by default if branch is not specified
			URL = URL + "/main"
		}

		// Convert host part of the URL
		if url.Host == "github.com" {
			URL = strings.Replace(URL, "github.com", "raw.githubusercontent.com", 1)
		}
	}

	return URL, nil
}
