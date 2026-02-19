/*
Copyright The Helm Authors.

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
package v3

import (
	"testing"
)

func TestValidateDependency(t *testing.T) {
	dep := &Dependency{
		Name: "example",
	}
	for value, shouldFail := range map[string]bool{
		"abcdefghijklmenopQRSTUVWXYZ-0123456780_": false,
		"-okay":      false,
		"_okay":      false,
		"- bad":      true,
		" bad":       true,
		"bad\nvalue": true,
		"bad ":       true,
		"bad$":       true,
	} {
		dep.Alias = value
		res := dep.Validate()
		if res != nil && !shouldFail {
			t.Errorf("Failed on case %q", dep.Alias)
		} else if res == nil && shouldFail {
			t.Errorf("Expected failure for %q", dep.Alias)
		}
	}
}

func TestValidateDependencyDependsOn(t *testing.T) {
	dep := &Dependency{
		Name:      "example",
		DependsOn: []string{"foo", "bar"},
	}
	if err := dep.Validate(); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(dep.DependsOn) != 2 {
		t.Errorf("Expected 2 depends-on entries, got %d", len(dep.DependsOn))
	}
}

func TestValidateDependencyDependsOnSanitization(t *testing.T) {
	dep := &Dependency{
		Name:      "example",
		DependsOn: []string{" foo\t", "\nbar "},
	}
	if err := dep.Validate(); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if dep.DependsOn[0] != " foo " {
		t.Errorf("Expected sanitized DependsOn[0], got %q", dep.DependsOn[0])
	}
}
