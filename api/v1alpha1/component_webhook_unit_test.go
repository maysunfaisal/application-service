//
// Copyright 2022 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComponentValidatingWebhook(t *testing.T) {

	originalComponent := Component{
		Spec: ComponentSpec{
			ComponentName: "component",
			Application:   "application",
			Context:       "context",
			Source: ComponentSource{
				ComponentSourceUnion: ComponentSourceUnion{
					GitSource: &GitSource{
						URL: "http://link",
					},
				},
			},
		},
	}

	tests := []struct {
		name       string
		updateComp Component
		err        string
	}{
		{
			name: "component name cannot be changed",
			err:  "component name cannot be updated to",
			updateComp: Component{
				Spec: ComponentSpec{
					ComponentName: "component1",
				},
			},
		},
		{
			name: "application name cannot be changed",
			err:  "application name cannot be updated to",
			updateComp: Component{
				Spec: ComponentSpec{
					ComponentName: "component",
					Application:   "application1",
				},
			},
		},
		{
			name: "context cannot be changed",
			err:  "context cannot be updated to",
			updateComp: Component{
				Spec: ComponentSpec{
					ComponentName: "component",
					Application:   "application",
					Context:       "context1",
				},
			},
		},
		{
			name: "git src cannot be changed",
			err:  "git source cannot be updated to",
			updateComp: Component{
				Spec: ComponentSpec{
					ComponentName: "component",
					Application:   "application",
					Context:       "context",
					Source: ComponentSource{
						ComponentSourceUnion: ComponentSourceUnion{
							GitSource: &GitSource{
								URL: "http://link1",
							},
						},
					},
				},
			},
		},
		{
			name: "image src can be changed",
			updateComp: Component{
				Spec: ComponentSpec{
					ComponentName: "component",
					Application:   "application",
					Context:       "context",
					Source: ComponentSource{
						ComponentSourceUnion: ComponentSourceUnion{
							ImageSource: &ImageSource{
								ContainerImage: "image1",
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err == "" {
				originalComponent = Component{
					Spec: ComponentSpec{
						ComponentName: "component",
						Application:   "application",
						Context:       "context",
						Source: ComponentSource{
							ComponentSourceUnion: ComponentSourceUnion{
								ImageSource: &ImageSource{
									ContainerImage: "image",
								},
							},
						},
					},
				}
			}
			err := test.updateComp.ValidateUpdate(&originalComponent)
			if test.err == "" {
				assert.Nil(t, err)
			} else {
				assert.Contains(t, err.Error(), test.err)
			}
		})
	}
}
