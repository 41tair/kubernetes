/*
Copyright 2020 The Kubernetes Authors.

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

package netpol

import (
	"github.com/onsi/ginkgo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

// ProbeJob packages the data for the input of a pod->pod connectivity probe
type ProbeJob struct {
	PodFrom        *Pod
	PodTo          *Pod
	FromPort       int
	ToPort         int
	ToPodDNSDomain string
	Protocol       v1.Protocol
}

// ProbeJobResults packages the data for the results of a pod->pod connectivity probe
type ProbeJobResults struct {
	Job         *ProbeJob
	IsConnected bool
	Err         error
	Command     string
}

// ProbePodToPodConnectivity runs a series of probes in kube, and records the results in `testCase.Reachability`
func ProbePodToPodConnectivity(k8s *kubeManager, model *Model, testCase *TestCase) {
	numberOfWorkers := 30
	allPods := model.AllPods()
	size := len(allPods) * len(allPods)
	jobs := make(chan *ProbeJob, size)
	results := make(chan *ProbeJobResults, size)
	for i := 0; i < numberOfWorkers; i++ {
		go probeWorker(k8s, jobs, results)
	}
	for _, podFrom := range allPods {
		for _, podTo := range allPods {
			jobs <- &ProbeJob{
				PodFrom:        podFrom,
				PodTo:          podTo,
				FromPort:       testCase.FromPort,
				ToPort:         testCase.ToPort,
				ToPodDNSDomain: model.DNSDomain,
				Protocol:       testCase.Protocol,
			}
		}
	}
	close(jobs)

	for i := 0; i < size; i++ {
		result := <-results
		job := result.Job
		if result.Err != nil {
			framework.Logf("unable to perform probe %s -> %s: %v", job.PodFrom.PodString(), job.PodTo.PodString(), result.Err)
		}
		testCase.Reachability.Observe(job.PodFrom.PodString(), job.PodTo.PodString(), result.IsConnected)
		expected := testCase.Reachability.Expected.Get(job.PodFrom.PodString().String(), job.PodTo.PodString().String())
		if result.IsConnected != expected {
			framework.Logf("Validation of %s -> %s FAILED !!!", job.PodFrom.PodString(), job.PodTo.PodString())
			framework.Logf("error %v ", result.Err)
			if expected {
				framework.Logf("Expected allowed pod connection was instead BLOCKED --- run '%v'", result.Command)
			} else {
				framework.Logf("Expected blocked pod connection was instead ALLOWED --- run '%v'", result.Command)
			}
		}
	}
}

// probeWorker continues polling a pod connectivity status, until the incoming "jobs" channel is closed, and writes results back out to the "results" channel.
// it only writes pass/fail status to a channel and has no failure side effects, this is by design since we do not want to fail inside a goroutine.
func probeWorker(k8s *kubeManager, jobs <-chan *ProbeJob, results chan<- *ProbeJobResults) {
	defer ginkgo.GinkgoRecover()
	for job := range jobs {
		podFrom := job.PodFrom
		containerFrom, err := podFrom.FindContainer(int32(job.FromPort), job.Protocol)
		// 1) sanity check that the pod container is found before we run the real test.
		if err != nil {
			result := &ProbeJobResults{
				Job:         job,
				IsConnected: false,
				Err:         err,
				Command:     "(skipped, pod unavailable)",
			}
			results <- result
		} else {
			// 2) real test runs here...
			connected, command, err := k8s.probeConnectivity(podFrom.Namespace, podFrom.Name, containerFrom.Name(), job.PodTo.QualifiedServiceAddress(job.ToPodDNSDomain), job.Protocol, job.ToPort)
			result := &ProbeJobResults{
				Job:         job,
				IsConnected: connected,
				Err:         err,
				Command:     command,
			}
			results <- result
		}
	}

}
