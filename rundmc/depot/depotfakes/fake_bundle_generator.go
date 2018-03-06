// Code generated by counterfeiter. DO NOT EDIT.
package depotfakes

import (
	"sync"

	"code.cloudfoundry.org/guardian/gardener/container-spec"
	"code.cloudfoundry.org/guardian/rundmc/depot"
	"code.cloudfoundry.org/guardian/rundmc/goci"
)

type FakeBundleGenerator struct {
	GenerateStub        func(desiredContainerSpec spec.DesiredContainerSpec, containerDir string) (goci.Bndl, error)
	generateMutex       sync.RWMutex
	generateArgsForCall []struct {
		desiredContainerSpec spec.DesiredContainerSpec
		containerDir         string
	}
	generateReturns struct {
		result1 goci.Bndl
		result2 error
	}
	generateReturnsOnCall map[int]struct {
		result1 goci.Bndl
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeBundleGenerator) Generate(desiredContainerSpec spec.DesiredContainerSpec, containerDir string) (goci.Bndl, error) {
	fake.generateMutex.Lock()
	ret, specificReturn := fake.generateReturnsOnCall[len(fake.generateArgsForCall)]
	fake.generateArgsForCall = append(fake.generateArgsForCall, struct {
		desiredContainerSpec spec.DesiredContainerSpec
		containerDir         string
	}{desiredContainerSpec, containerDir})
	fake.recordInvocation("Generate", []interface{}{desiredContainerSpec, containerDir})
	fake.generateMutex.Unlock()
	if fake.GenerateStub != nil {
		return fake.GenerateStub(desiredContainerSpec, containerDir)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	return fake.generateReturns.result1, fake.generateReturns.result2
}

func (fake *FakeBundleGenerator) GenerateCallCount() int {
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	return len(fake.generateArgsForCall)
}

func (fake *FakeBundleGenerator) GenerateArgsForCall(i int) (spec.DesiredContainerSpec, string) {
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	return fake.generateArgsForCall[i].desiredContainerSpec, fake.generateArgsForCall[i].containerDir
}

func (fake *FakeBundleGenerator) GenerateReturns(result1 goci.Bndl, result2 error) {
	fake.GenerateStub = nil
	fake.generateReturns = struct {
		result1 goci.Bndl
		result2 error
	}{result1, result2}
}

func (fake *FakeBundleGenerator) GenerateReturnsOnCall(i int, result1 goci.Bndl, result2 error) {
	fake.GenerateStub = nil
	if fake.generateReturnsOnCall == nil {
		fake.generateReturnsOnCall = make(map[int]struct {
			result1 goci.Bndl
			result2 error
		})
	}
	fake.generateReturnsOnCall[i] = struct {
		result1 goci.Bndl
		result2 error
	}{result1, result2}
}

func (fake *FakeBundleGenerator) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.generateMutex.RLock()
	defer fake.generateMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeBundleGenerator) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ depot.BundleGenerator = new(FakeBundleGenerator)
