// Copyright (C) 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vulkan

import (
	"context"

	"github.com/google/gapid/core/log"
	"github.com/google/gapid/core/math/interval"
	"github.com/google/gapid/gapis/api"
	"github.com/google/gapid/gapis/memory"
)

type stateBuilder struct {
	ctx             context.Context
	s               *State
	oldState        *api.GlobalState
	newState        *api.GlobalState
	cmds            []api.Cmd
	cb              CommandBuilder
	readMemories    []*api.AllocResult
	writeMemories   []*api.AllocResult
	memoryIntervals interval.U64RangeList
}

// TODO: wherever possible, use old resources instead of doing full reads on the old pools.
//       This is especially useful for things that are internal pools, (Shader words for example)
func (s *State) RebuildState(ctx context.Context, oldState *api.GlobalState) ([]api.Cmd, interval.U64RangeList) {
	// TODO: Debug Info
	newState := api.NewStateWithAllocator(memory.NewBasicAllocator(oldState.Allocator.FreeList()), oldState.MemoryLayout)
	sb := &stateBuilder{
		ctx:             ctx,
		s:               s,
		oldState:        oldState,
		newState:        newState,
		cb:              CommandBuilder{Thread: 0},
		memoryIntervals: interval.U64RangeList{},
	}

	sb.newState.Memory.NewAt(sb.oldState.Memory.NextPoolID())

	for _, k := range s.Instances().Keys() {
		sb.createInstance(k, s.Instances().Get(k))
	}

	sb.createPhysicalDevices(s.PhysicalDevices())

	for _, su := range s.Surfaces().Keys() {
		sb.createSurface(s.Surfaces().Get(su))
	}

	for _, d := range s.Devices().Keys() {
		sb.createDevice(s.Devices().Get(d))
	}

	for _, q := range s.Queues().Keys() {
		sb.createQueue(s.Queues().Get(q))
	}

	for _, swp := range s.Swapchains().Keys() {
		sb.createSwapchain(s.Swapchains().Get(swp))
	}

	// Create all non-dedicated allocations.
	// Dedicated allocations will be created with their
	// objects
	for _, mem := range s.DeviceMemories().Keys() {
		// TODO: Handle KHR dedicated allocation as well as NV
		sb.createDeviceMemory(s.DeviceMemories().Get(mem), false)
	}

	for _, buf := range s.Buffers().Keys() {
		sb.createBuffer(s.Buffers().Get(buf))
	}

	{
		imgPrimer := newImagePrimer(sb)
		for _, img := range s.Images().Keys() {
			sb.createImage(s.Images().Get(img), imgPrimer)
		}
		imgPrimer.free()
	}

	for _, smp := range s.Samplers().Keys() {
		sb.createSampler(s.Samplers().Get(smp))
	}

	for _, fnc := range s.Fences().Keys() {
		sb.createFence(s.Fences().Get(fnc))
	}

	for _, sem := range s.Semaphores().Keys() {
		sb.createSemaphore(s.Semaphores().Get(sem))
	}

	for _, evt := range s.Events().Keys() {
		sb.createEvent(s.Events().Get(evt))
	}

	for _, cp := range s.CommandPools().Keys() {
		sb.createCommandPool(s.CommandPools().Get(cp))
	}

	for _, pc := range s.PipelineCaches().Keys() {
		sb.createPipelineCache(s.PipelineCaches().Get(pc))
	}

	for _, dsl := range s.DescriptorSetLayouts().Keys() {
		sb.createDescriptorSetLayout(s.DescriptorSetLayouts().Get(dsl))
	}

	for _, pl := range s.PipelineLayouts().Keys() {
		sb.createPipelineLayout(s.PipelineLayouts().Get(pl))
	}

	for _, rp := range s.RenderPasses().Keys() {
		sb.createRenderPass(s.RenderPasses().Get(rp))
	}

	for _, sm := range s.ShaderModules().Keys() {
		sb.createShaderModule(s.ShaderModules().Get(sm))
	}

	for _, cp := range getPipelinesInOrder(s, true) {
		sb.createComputePipeline(s.ComputePipelines().Get(cp))
	}

	for _, gp := range getPipelinesInOrder(s, false) {
		sb.createGraphicsPipeline(s.GraphicsPipelines().Get(gp))
	}

	for _, iv := range s.ImageViews().Keys() {
		sb.createImageView(s.ImageViews().Get(iv))
	}

	for _, bv := range s.BufferViews().Keys() {
		sb.createBufferView(s.BufferViews().Get(bv))
	}

	for _, dp := range s.DescriptorPools().Keys() {
		sb.createDescriptorPool(s.DescriptorPools().Get(dp))
	}

	for _, fb := range s.Framebuffers().Keys() {
		sb.createFramebuffer(s.Framebuffers().Get(fb))
	}

	for _, fb := range s.DescriptorSets().Keys() {
		sb.createDescriptorSet(s.DescriptorSets().Get(fb))
	}

	for _, qp := range s.QueryPools().Keys() {
		sb.createQueryPool(s.QueryPools().Get(qp))
	}

	for _, qp := range s.CommandBuffers().Keys() {
		sb.createCommandBuffer(s.CommandBuffers().Get(qp), VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_SECONDARY)
	}

	for _, qp := range s.CommandBuffers().Keys() {
		sb.createCommandBuffer(s.CommandBuffers().Get(qp), VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_PRIMARY)
	}

	return sb.cmds, sb.memoryIntervals
}

func getPipelinesInOrder(s *State, compute bool) []VkPipeline {
	pipelines := []VkPipeline{}
	unhandledPipelines := map[VkPipeline]VkPipeline{}
	handledPipelines := map[VkPipeline]bool{}
	if compute {
		for _, p := range s.ComputePipelines().Keys() {
			pp := s.ComputePipelines().Get(p)
			unhandledPipelines[pp.VulkanHandle()] = pp.BasePipeline()
		}
	} else {
		for _, p := range s.GraphicsPipelines().Keys() {
			pp := s.GraphicsPipelines().Get(p)
			unhandledPipelines[pp.VulkanHandle()] = pp.BasePipeline()
		}
	}

	numHandled := 0
	for len(unhandledPipelines) != 0 {
		for k, v := range unhandledPipelines {
			handled := false
			if v == 0 {
				pipelines = append(pipelines, k)
				handled = true
			} else if _, ok := handledPipelines[v]; ok {
				pipelines = append(pipelines, k)
				handled = true
			}
			if handled {
				handledPipelines[k] = true
				delete(unhandledPipelines, k)
				numHandled++
			}
		}
		if numHandled == 0 {
			// There is a cycle in the basePipeline indices.
			// Or the no base pipelines does exist.
			// Create the rest without base pipelines
			for k := range unhandledPipelines {
				pipelines = append(pipelines, k)
			}
			unhandledPipelines = map[VkPipeline]VkPipeline{}
			break
		}
	}
	return pipelines
}

func (sb *stateBuilder) MustAllocReadData(v ...interface{}) api.AllocResult {
	allocateResult := sb.newState.AllocDataOrPanic(sb.ctx, v...)
	sb.readMemories = append(sb.readMemories, &allocateResult)
	rng := allocateResult.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocateResult
}

func (sb *stateBuilder) MustAllocWriteData(v ...interface{}) api.AllocResult {
	allocateResult := sb.newState.AllocDataOrPanic(sb.ctx, v...)
	sb.writeMemories = append(sb.writeMemories, &allocateResult)
	rng := allocateResult.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocateResult
}

func (sb *stateBuilder) MustUnpackReadMap(v interface{}) api.AllocResult {
	allocateResult, _ := unpackMap(sb.ctx, sb.newState, v)
	sb.readMemories = append(sb.readMemories, &allocateResult)
	rng := allocateResult.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocateResult
}

func (sb *stateBuilder) MustUnpackWriteMap(v interface{}) api.AllocResult {
	allocateResult, _ := unpackMap(sb.ctx, sb.newState, v)
	sb.writeMemories = append(sb.writeMemories, &allocateResult)
	rng := allocateResult.Range()
	interval.Merge(&sb.memoryIntervals, interval.U64Span{rng.Base, rng.Base + rng.Size}, true)
	return allocateResult
}

func (sb *stateBuilder) getCommandBuffer(queue QueueObjectʳ) (VkCommandBuffer, VkCommandPool) {
	commandBufferID := VkCommandBuffer(newUnusedID(true, func(x uint64) bool { return sb.s.CommandBuffers().Contains(VkCommandBuffer(x)) }))
	commandPoolID := VkCommandPool(newUnusedID(true, func(x uint64) bool { return sb.s.CommandPools().Contains(VkCommandPool(x)) }))

	sb.write(sb.cb.VkCreateCommandPool(
		queue.Device(),
		sb.MustAllocReadData(NewVkCommandPoolCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO, // sType
			0,              // pNext
			0,              // flags
			queue.Family(), // queueFamilyIndex
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(commandPoolID).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkAllocateCommandBuffers(
		queue.Device(),
		sb.MustAllocReadData(NewVkCommandBufferAllocateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO, // sType
			0,             // pNext
			commandPoolID, // commandPool
			VkCommandBufferLevel_VK_COMMAND_BUFFER_LEVEL_PRIMARY, // level
			uint32(1), // commandBufferCount
		)).Ptr(),
		sb.MustAllocWriteData(commandBufferID).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkBeginCommandBuffer(
		commandBufferID,
		sb.MustAllocReadData(NewVkCommandBufferBeginInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, // sType
			0, // pNext
			0, // flags
			0, // pInheritanceInfo
		)).Ptr(),
		VkResult_VK_SUCCESS,
	))

	return commandBufferID, commandPoolID
}

func (sb *stateBuilder) endSubmitAndDestroyCommandBuffer(queue QueueObjectʳ, commandBuffer VkCommandBuffer, commandPool VkCommandPool) {
	sb.write(sb.cb.VkEndCommandBuffer(
		commandBuffer,
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkQueueSubmit(
		queue.VulkanHandle(),
		1,
		sb.MustAllocReadData(NewVkSubmitInfo(
			VkStructureType_VK_STRUCTURE_TYPE_SUBMIT_INFO, // sType
			0, // pNext
			0, // waitSemaphoreCount
			0, // pWaitSemaphores
			0, // pWaitDstStageMask
			1, // commandBufferCount
			NewVkCommandBufferᶜᵖ(sb.MustAllocReadData(commandBuffer).Ptr()), // pCommandBuffers
			0, // signalSemaphoreCount
			0, // pSignalSemaphores
		)).Ptr(),
		VkFence(0),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkQueueWaitIdle(queue.VulkanHandle(), VkResult_VK_SUCCESS))
	sb.write(sb.cb.VkDestroyCommandPool(
		queue.Device(),
		commandPool,
		memory.Nullptr,
	))
}

func (sb *stateBuilder) write(cmd api.Cmd) {
	for _, read := range sb.readMemories {
		cmd.Extras().GetOrAppendObservations().AddRead(read.Data())
	}
	for _, write := range sb.writeMemories {
		cmd.Extras().GetOrAppendObservations().AddWrite(write.Data())
	}

	if err := cmd.Mutate(sb.ctx, api.CmdNoID, sb.newState, nil); err != nil {
		log.W(sb.ctx, "Initial cmd %v: %v - %v", len(sb.cmds), cmd, err)
	} else {
		log.D(sb.ctx, "Initial cmd %v: %v", len(sb.cmds), cmd)
	}
	sb.cmds = append(sb.cmds, cmd)
	for _, read := range sb.readMemories {
		read.Free()
	}
	for _, write := range sb.writeMemories {
		write.Free()
	}
	sb.readMemories = []*api.AllocResult{}
	sb.writeMemories = []*api.AllocResult{}
}

func (sb *stateBuilder) createInstance(vk VkInstance, inst InstanceObjectʳ) {
	enabledLayers := []Charᶜᵖ{}
	for _, layer := range inst.EnabledLayers().Range() {
		enabledLayers = append(enabledLayers, NewCharᶜᵖ(sb.MustAllocReadData(layer).Ptr()))
	}
	enabledExtensions := []Charᶜᵖ{}
	for _, ext := range inst.EnabledExtensions().Range() {
		enabledExtensions = append(enabledExtensions, NewCharᶜᵖ(sb.MustAllocReadData(ext).Ptr()))
	}

	sb.write(sb.cb.VkCreateInstance(
		sb.MustAllocReadData(NewVkInstanceCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			0, // pApplicationInfo
			uint32(inst.EnabledLayers().Len()),                         // enabledLayerCount
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledLayers).Ptr()),     // ppEnabledLayerNames
			uint32(inst.EnabledExtensions().Len()),                     // enabledExtensionCount
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledExtensions).Ptr()), // ppEnabledExtensionNames
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(vk).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPhysicalDevices(Map VkPhysicalDeviceːPhysicalDeviceObjectʳᵐ) {
	devices := map[VkInstance][]VkPhysicalDevice{}
	for _, k := range Map.Keys() {
		v := Map.Get(k)
		_, ok := devices[v.Instance()]
		if !ok {
			devices[v.Instance()] = []VkPhysicalDevice{}
		}

		devices[v.Instance()] = append(devices[v.Instance()], k)
	}

	for i, devs := range devices {
		sb.write(sb.cb.VkEnumeratePhysicalDevices(
			i,
			NewU32ᶜᵖ(sb.MustAllocWriteData(len(devs)).Ptr()),
			NewVkPhysicalDeviceᵖ(memory.Nullptr),
			VkResult_VK_SUCCESS,
		))
		sb.write(sb.cb.VkEnumeratePhysicalDevices(
			i,
			NewU32ᶜᵖ(sb.MustAllocReadData(len(devs)).Ptr()),
			NewVkPhysicalDeviceᵖ(sb.MustAllocReadData(devs).Ptr()),
			VkResult_VK_SUCCESS,
		))

		for _, device := range devs {
			pd := Map.Get(device)
			sb.write(sb.cb.VkGetPhysicalDeviceProperties(
				device,
				NewVkPhysicalDevicePropertiesᵖ(sb.MustAllocWriteData(pd.PhysicalDeviceProperties()).Ptr()),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceMemoryProperties(
				device,
				NewVkPhysicalDeviceMemoryPropertiesᵖ(sb.MustAllocWriteData(pd.MemoryProperties()).Ptr()),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceQueueFamilyProperties(
				device,
				NewU32ᶜᵖ(sb.MustAllocWriteData(pd.QueueFamilyProperties().Len()).Ptr()),
				NewVkQueueFamilyPropertiesᵖ(memory.Nullptr),
			))
			sb.write(sb.cb.VkGetPhysicalDeviceQueueFamilyProperties(
				device,
				NewU32ᶜᵖ(sb.MustAllocReadData(pd.QueueFamilyProperties().Len()).Ptr()),
				NewVkQueueFamilyPropertiesᵖ(sb.MustUnpackWriteMap(pd.QueueFamilyProperties()).Ptr()),
			))
		}
	}
}

func (sb *stateBuilder) createSurface(s SurfaceObjectʳ) {
	switch s.Type() {
	case SurfaceType_SURFACE_TYPE_XCB:
		sb.write(sb.cb.VkCreateXcbSurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkXcbSurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_XCB_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // connection
				0, // window
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_ANDROID:
		sb.write(sb.cb.VkCreateAndroidSurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkAndroidSurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_ANDROID_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // window
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_WIN32:
		sb.write(sb.cb.VkCreateWin32SurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkWin32SurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_WIN32_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // hinstance
				0, // hwnd
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_WAYLAND:
		sb.write(sb.cb.VkCreateWaylandSurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkWaylandSurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // display
				0, // surface
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_XLIB:
		sb.write(sb.cb.VkCreateXlibSurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkXlibSurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_XLIB_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // dpy
				0, // window
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	case SurfaceType_SURFACE_TYPE_MIR:
		sb.write(sb.cb.VkCreateMirSurfaceKHR(
			s.Instance(),
			sb.MustAllocReadData(NewVkMirSurfaceCreateInfoKHR(
				VkStructureType_VK_STRUCTURE_TYPE_MIR_SURFACE_CREATE_INFO_KHR, // sType
				0, // pNext
				0, // flags
				0, // connection
				0, // mirSurface
			)).Ptr(),
			memory.Nullptr,
			sb.MustAllocWriteData(s.VulkanHandle()).Ptr(),
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) createDevice(d DeviceObjectʳ) {
	enabledLayers := []Charᶜᵖ{}
	for _, layer := range d.EnabledLayers().Range() {
		enabledLayers = append(enabledLayers, NewCharᶜᵖ(sb.MustAllocReadData(layer).Ptr()))
	}
	enabledExtensions := []Charᶜᵖ{}
	for _, ext := range d.EnabledExtensions().Range() {
		enabledExtensions = append(enabledExtensions, NewCharᶜᵖ(sb.MustAllocReadData(ext).Ptr()))
	}

	queueCreate := map[uint32]VkDeviceQueueCreateInfo{}
	queuePriorities := map[uint32][]float32{}

	for _, q := range d.Queues().Range() {
		if _, ok := queueCreate[q.QueueFamilyIndex()]; !ok {
			queueCreate[q.QueueFamilyIndex()] = NewVkDeviceQueueCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO, // sType
				0,                    // pNext
				0,                    // flags
				q.QueueFamilyIndex(), // queueFamilyIndex
				0,                    // queueCount
				0,                    // pQueuePriorities - This gets filled in later
			)
			queuePriorities[q.QueueFamilyIndex()] = []float32{}
		}
		x := queueCreate[q.QueueFamilyIndex()]
		x.SetQueueCount(x.QueueCount() + 1)
		queueCreate[q.QueueFamilyIndex()] = x
		if uint32(len(queuePriorities[q.QueueFamilyIndex()])) < q.QueueIndex()+1 {
			t := make([]float32, q.QueueIndex()+1)
			copy(t, queuePriorities[q.QueueFamilyIndex()])
			queuePriorities[q.QueueFamilyIndex()] = t
		}
		queuePriorities[q.QueueFamilyIndex()][q.QueueIndex()] = q.Priority()
	}
	reorderedQueueCreates := map[uint32]VkDeviceQueueCreateInfo{}
	i := uint32(0)
	for k, v := range queueCreate {
		v.SetPQueuePriorities(NewF32ᶜᵖ(sb.MustAllocReadData(queuePriorities[k]).Ptr()))
		reorderedQueueCreates[i] = v
		i++
	}

	sb.write(sb.cb.VkCreateDevice(
		d.PhysicalDevice(),
		sb.MustAllocReadData(NewVkDeviceCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			uint32(len(reorderedQueueCreates)),                                              // queueCreateInfoCount
			NewVkDeviceQueueCreateInfoᶜᵖ(sb.MustUnpackReadMap(reorderedQueueCreates).Ptr()), // pQueueCreateInfos
			uint32(len(enabledLayers)),                                                      // enabledLayerCount
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledLayers).Ptr()),                          // ppEnabledLayerNames
			uint32(len(enabledExtensions)),                                                  // enabledExtensionCount
			NewCharᶜᵖᶜᵖ(sb.MustAllocReadData(enabledExtensions).Ptr()),                      // ppEnabledExtensionNames
			NewVkPhysicalDeviceFeaturesᶜᵖ(sb.MustAllocReadData(d.EnabledFeatures()).Ptr()),  // pEnabledFeatures
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(d.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createQueue(q QueueObjectʳ) {
	sb.write(sb.cb.VkGetDeviceQueue(
		q.Device(),
		q.Family(),
		q.Index(),
		sb.MustAllocWriteData(q.VulkanHandle()).Ptr(),
	))
}

func (sb *stateBuilder) transitionImage(image ImageObjectʳ,
	oldLayout, newLayout VkImageLayout,
	oldQueue, newQueue QueueObjectʳ) {

	if image.LastBoundQueue().IsNil() {
		// We cannot transition an image that has never been
		// on a queue
		return
	}
	commandBuffer, commandPool := sb.getCommandBuffer(image.LastBoundQueue())

	newFamily := newQueue.Family()
	oldFamily := newQueue.Family()
	if !oldQueue.IsNil() {
		oldFamily = oldQueue.Family()
	}

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		0,
		memory.Nullptr,
		0,
		memory.Nullptr,
		1,
		sb.MustAllocReadData(NewVkImageMemoryBarrier(
			VkStructureType_VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER, // sType
			0, // pNext
			VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // srcAccessMask
			VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // dstAccessMask
			oldLayout,            // oldLayout
			newLayout,            // newLayout
			oldFamily,            // srcQueueFamilyIndex
			newFamily,            // dstQueueFamilyIndex
			image.VulkanHandle(), // image
			NewVkImageSubresourceRange( // subresourceRange
				image.ImageAspect(),
				0,
				image.Info().MipLevels(),
				0,
				image.Info().ArrayLayers(),
			),
		)).Ptr(),
	))

	sb.endSubmitAndDestroyCommandBuffer(newQueue, commandBuffer, commandPool)
}

func (sb *stateBuilder) createSwapchain(swp SwapchainObjectʳ) {
	extent := NewVkExtent2D(
		swp.Info().Extent().Width(),
		swp.Info().Extent().Height(),
	)
	sb.write(sb.cb.VkCreateSwapchainKHR(
		swp.Device(),
		sb.MustAllocReadData(NewVkSwapchainCreateInfoKHR(
			VkStructureType_VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR, // sType
			0, // pNext
			0, // flags
			swp.Surface().VulkanHandle(),        // surface
			uint32(swp.SwapchainImages().Len()), // minImageCount
			swp.Info().Fmt(),                    // imageFormat
			swp.ColorSpace(),                    // imageColorSpace
			extent,                              // imageExtent
			swp.Info().ArrayLayers(),                                              // imageArrayLayers
			swp.Info().Usage(),                                                    // imageUsage
			swp.Info().SharingMode(),                                              // imageSharingMode
			uint32(swp.Info().QueueFamilyIndices().Len()),                         // queueFamilyIndexCount
			NewU32ᶜᵖ(sb.MustUnpackReadMap(swp.Info().QueueFamilyIndices()).Ptr()), // pQueueFamilyIndices
			swp.PreTransform(),   // preTransform
			swp.CompositeAlpha(), // compositeAlpha
			swp.PresentMode(),    // presentMode
			swp.Clipped(),        // clipped
			0,                    // oldSwapchain
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(swp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkGetSwapchainImagesKHR(
		swp.Device(),
		swp.VulkanHandle(),
		NewU32ᶜᵖ(sb.MustAllocWriteData(uint32(swp.SwapchainImages().Len())).Ptr()),
		memory.Nullptr,
		VkResult_VK_SUCCESS,
	))

	images := []VkImage{}
	for _, v := range swp.SwapchainImages().Keys() {
		images = append(images, swp.SwapchainImages().Get(v).VulkanHandle())
	}

	sb.write(sb.cb.VkGetSwapchainImagesKHR(
		swp.Device(),
		swp.VulkanHandle(),
		NewU32ᶜᵖ(sb.MustAllocReadData(uint32(swp.SwapchainImages().Len())).Ptr()),
		sb.MustAllocWriteData(images).Ptr(),
		VkResult_VK_SUCCESS,
	))
	for _, v := range swp.SwapchainImages().Range() {
		q := sb.getQueueFor(v.LastBoundQueue(), v.Device(), v.Info().QueueFamilyIndices().Range())
		sb.transitionImage(v, VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED,
			v.Info().Layout(), NilQueueObjectʳ, q)
	}
}

func (sb *stateBuilder) createDeviceMemory(mem DeviceMemoryObjectʳ, allowDedicatedNV bool) {
	if !allowDedicatedNV && !mem.DedicatedAllocationNV().IsNil() {
		return
	}

	pNext := NewVoidᶜᵖ(memory.Nullptr)

	if !mem.DedicatedAllocationNV().IsNil() {
		pNext = NewVoidᶜᵖ(sb.MustAllocReadData(
			NewVkDedicatedAllocationMemoryAllocateInfoNV(
				VkStructureType_VK_STRUCTURE_TYPE_DEDICATED_ALLOCATION_MEMORY_ALLOCATE_INFO_NV, // sType
				0, // pNext
				mem.DedicatedAllocationNV().Image(),  // image
				mem.DedicatedAllocationNV().Buffer(), // buffer
			),
		).Ptr())
	}

	sb.write(sb.cb.VkAllocateMemory(
		mem.Device(),
		NewVkMemoryAllocateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkMemoryAllocateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, // sType
				pNext,                 // pNext
				mem.AllocationSize(),  // allocationSize
				mem.MemoryTypeIndex(), // memoryTypeIndex
			)).Ptr()),
		memory.Nullptr,
		sb.MustAllocWriteData(mem.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if mem.MappedLocation().Address() != 0 {
		sb.write(sb.cb.VkMapMemory(
			mem.Device(),
			mem.VulkanHandle(),
			mem.MappedOffset(),
			mem.MappedSize(),
			VkMemoryMapFlags(0),
			NewVoidᵖᵖ(sb.MustAllocWriteData(mem.MappedLocation()).Ptr()),
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) GetScratchBufferMemoryIndex(device DeviceObjectʳ) uint32 {
	physicalDeviceObject := sb.s.PhysicalDevices().Get(device.PhysicalDevice())

	typeBits := uint32((uint64(1) << uint64(physicalDeviceObject.MemoryProperties().MemoryTypeCount())) - 1)
	if sb.s.TransferBufferMemoryRequirements().Contains(device.VulkanHandle()) {
		typeBits = sb.s.TransferBufferMemoryRequirements().Get(device.VulkanHandle()).MemoryTypeBits()
	}
	index := memoryTypeIndexFor(typeBits, physicalDeviceObject.MemoryProperties(), VkMemoryPropertyFlags(VkMemoryPropertyFlagBits_VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT))
	if index >= 0 {
		return uint32(index)
	}
	log.E(sb.ctx, "cannnot get the memory type index for host visible memory to create scratch buffer, fallback to use index 0")
	return 0
}

// Find the index of the memory type that satisfies the specified memory property
// flags.
func memoryTypeIndexFor(memTypeBits uint32, props VkPhysicalDeviceMemoryProperties, flags VkMemoryPropertyFlags) int {
	for i := 0; i < int(props.MemoryTypeCount()); i++ {
		if (memTypeBits & (1 << uint(i))) == 0 {
			continue
		}
		t := props.MemoryTypes().Get(i)
		if flags == (t.PropertyFlags() & flags) {
			return i
		}
	}
	return -1
}

func (sb *stateBuilder) allocAndFillScratchBuffer(device DeviceObjectʳ, data []uint8, usages ...VkBufferUsageFlagBits) (VkBuffer, VkDeviceMemory) {
	buffer := VkBuffer(newUnusedID(true, func(x uint64) bool { return sb.s.Buffers().Contains(VkBuffer(x)) }))
	deviceMemory := VkDeviceMemory(newUnusedID(true, func(x uint64) bool { return sb.s.DeviceMemories().Contains(VkDeviceMemory(x)) }))

	size := VkDeviceSize(len(data))
	usageFlags := VkBufferUsageFlags(VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_SRC_BIT)
	for _, u := range usages {
		usageFlags |= VkBufferUsageFlags(u)
	}

	sb.write(sb.cb.VkCreateBuffer(
		device.VulkanHandle(),
		sb.MustAllocReadData(
			NewVkBufferCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO, // sType
				0,                                       // pNext
				0,                                       // flags
				size,                                    // size
				usageFlags,                              // usage
				VkSharingMode_VK_SHARING_MODE_EXCLUSIVE, // sharingMode
				0, // queueFamilyIndexCount
				0, // pQueueFamilyIndices
			)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(buffer).Ptr(),
		VkResult_VK_SUCCESS,
	))

	memoryTypeIndex := sb.GetScratchBufferMemoryIndex(device)

	// Since we cannot guess how much the driver will actually request of us,
	// overallocate by a factor of 2. This should be enough.
	// Align to 0x100 to make validation layers happy. Assuming the buffer memory
	// requirement has an alignment value compatible with 0x100.
	allocSize := VkDeviceSize((uint64(size*2) + uint64(255)) & ^uint64(255))

	// Make sure we allocate a buffer that is more than big enough for the
	// data
	sb.write(sb.cb.VkAllocateMemory(
		device.VulkanHandle(),
		NewVkMemoryAllocateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkMemoryAllocateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, // sType
				0,               // pNext
				allocSize,       // allocationSize
				memoryTypeIndex, // memoryTypeIndex
			)).Ptr()),
		memory.Nullptr,
		sb.MustAllocWriteData(deviceMemory).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkBindBufferMemory(
		device.VulkanHandle(),
		buffer,
		deviceMemory,
		0,
		VkResult_VK_SUCCESS,
	))

	dat := sb.newState.AllocDataOrPanic(sb.ctx, data)
	at := NewVoidᵖ(dat.Ptr())
	atdata := sb.newState.AllocDataOrPanic(sb.ctx, at)

	sb.write(sb.cb.VkMapMemory(
		device.VulkanHandle(),
		deviceMemory,
		VkDeviceSize(0),
		size,
		VkMemoryMapFlags(0),
		atdata.Ptr(),
		VkResult_VK_SUCCESS,
	).AddRead(atdata.Data()).AddWrite(atdata.Data()))

	sb.write(sb.cb.VkFlushMappedMemoryRanges(
		device.VulkanHandle(),
		1,
		sb.MustAllocReadData(NewVkMappedMemoryRange(
			VkStructureType_VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE, // sType
			0,            // pNext
			deviceMemory, // memory
			0,            // offset
			size,         // size
		)).Ptr(),
		VkResult_VK_SUCCESS,
	).AddRead(dat.Data()))

	sb.write(sb.cb.VkUnmapMemory(
		device.VulkanHandle(),
		deviceMemory,
	))

	dat.Free()
	atdata.Free()

	return buffer, deviceMemory
}

func (sb *stateBuilder) freeScratchBuffer(device DeviceObjectʳ, buffer VkBuffer, mem VkDeviceMemory) {
	sb.write(sb.cb.VkDestroyBuffer(device.VulkanHandle(), buffer, memory.Nullptr))
	sb.write(sb.cb.VkFreeMemory(device.VulkanHandle(), mem, memory.Nullptr))
}

func (sb *stateBuilder) getSparseQueueFor(lastBoundQueue QueueObjectʳ, device VkDevice, queueFamilyIndices map[uint32]uint32) QueueObjectʳ {
	hasQueueFamilyIndices := queueFamilyIndices != nil

	if !lastBoundQueue.IsNil() {
		queueProperties := sb.s.PhysicalDevices().Get(sb.s.Devices().Get(lastBoundQueue.Device()).PhysicalDevice()).QueueFamilyProperties()
		if 0 != (uint32(queueProperties.Get(lastBoundQueue.Family()).QueueFlags()) & uint32(VkQueueFlagBits_VK_QUEUE_SPARSE_BINDING_BIT)) {
			return lastBoundQueue
		}
	}

	dev := sb.s.Devices().Get(device)
	if dev.IsNil() {
		return lastBoundQueue
	}
	phyDev := sb.s.PhysicalDevices().Get(dev.PhysicalDevice())
	if phyDev.IsNil() {
		return lastBoundQueue
	}

	queueProperties := sb.s.PhysicalDevices().Get(sb.s.Devices().Get(device).PhysicalDevice()).QueueFamilyProperties()

	if hasQueueFamilyIndices {
		for _, v := range sb.s.Queues().Range() {
			if v.Device() != device {
				continue
			}
			if 0 != (uint32(queueProperties.Get(v.Family()).QueueFlags()) & uint32(VkQueueFlagBits_VK_QUEUE_SPARSE_BINDING_BIT)) {
				for _, i := range queueFamilyIndices {
					if i == v.Family() {
						return v
					}
				}
			}
		}
	}
	return lastBoundQueue
}

func (sb *stateBuilder) getQueueFor(lastBoundQueue QueueObjectʳ, device VkDevice, queueFamilyIndices map[uint32]uint32) QueueObjectʳ {
	if !lastBoundQueue.IsNil() {
		return lastBoundQueue
	}
	hasQueueFamilyIndices := queueFamilyIndices != nil

	if hasQueueFamilyIndices {
		for _, v := range sb.s.Queues().Range() {
			if v.Device() != device {
				continue
			}
			for _, i := range queueFamilyIndices {
				if i == v.Family() {
					return v
				}
			}
		}
	}

	for _, v := range sb.s.Queues().Range() {
		if v.Device() == device {
			return v
		}
	}
	return lastBoundQueue
}

func (sb *stateBuilder) createBuffer(buffer BufferObjectʳ) {
	os := sb.s
	pNext := NewVoidᶜᵖ(memory.Nullptr)

	if !buffer.Info().DedicatedAllocationNV().IsNil() {
		pNext = NewVoidᶜᵖ(sb.MustAllocReadData(
			NewVkDedicatedAllocationBufferCreateInfoNV(
				VkStructureType_VK_STRUCTURE_TYPE_DEDICATED_ALLOCATION_BUFFER_CREATE_INFO_NV, // sType
				0, // pNext
				buffer.Info().DedicatedAllocationNV().DedicatedAllocation(), // dedicatedAllocation
			),
		).Ptr())
	}

	denseBound := !buffer.Memory().IsNil()
	sparseBound := buffer.SparseMemoryBindings().Len() > 0
	sparseBinding :=
		(uint64(buffer.Info().CreateFlags()) &
			uint64(VkBufferCreateFlagBits_VK_BUFFER_CREATE_SPARSE_BINDING_BIT)) != 0
	sparseResidency :=
		sparseBinding &&
			(uint64(buffer.Info().CreateFlags())&
				uint64(VkBufferCreateFlagBits_VK_BUFFER_CREATE_SPARSE_RESIDENCY_BIT)) != 0

	sb.write(sb.cb.VkCreateBuffer(
		buffer.Device(),
		sb.MustAllocReadData(
			NewVkBufferCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO, // sType
				pNext, // pNext
				buffer.Info().CreateFlags(), // flags
				buffer.Info().Size(),        // size
				VkBufferUsageFlags(uint32(buffer.Info().Usage())|uint32(VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_DST_BIT)), // usage
				buffer.Info().SharingMode(),                                                      // sharingMode
				uint32(buffer.Info().QueueFamilyIndices().Len()),                                 // queueFamilyIndexCount
				NewU32ᶜᵖ(sb.MustUnpackReadMap(buffer.Info().QueueFamilyIndices().Range()).Ptr()), // pQueueFamilyIndices
			)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(buffer.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	sb.write(sb.cb.VkGetBufferMemoryRequirements(
		buffer.Device(),
		buffer.VulkanHandle(),
		sb.MustAllocWriteData(buffer.MemoryRequirements()).Ptr(),
	))

	// Dedicated allocation buffer/image must NOT be a sparse binding one.
	// Checking the dedicated allocation info on both the memory and the buffer
	// side, because we've found applications that do miss one of them.
	dedicatedMemoryNV := !buffer.Memory().IsNil() && (!buffer.Info().DedicatedAllocationNV().IsNil() || !buffer.Memory().DedicatedAllocationNV().IsNil())
	// Emit error message to report view if we found one of the dedicate allocation
	// info struct is missing.
	if dedicatedMemoryNV && buffer.Info().DedicatedAllocationNV().IsNil() {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkBuffer", uint64(buffer.VulkanHandle()))
	}
	if dedicatedMemoryNV && buffer.Memory().DedicatedAllocationNV().IsNil() {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkDeviceMemory", uint64(buffer.Memory().VulkanHandle()))
	}

	if dedicatedMemoryNV {
		sb.createDeviceMemory(buffer.Memory(), true)
	}

	if !denseBound && !sparseBound {
		return
	}

	contents := []uint8{}

	copies := []VkBufferCopy{}
	offset := VkDeviceSize(0)

	queue := sb.getQueueFor(buffer.LastBoundQueue(), buffer.Device(), buffer.Info().QueueFamilyIndices().Range())

	oldFamilyIndex := -1

	if buffer.SparseMemoryBindings().Len() > 0 {
		// If this buffer has sparse memory bindings, then we have to set them all
		// now
		if queue.IsNil() {
			return
		}
		memories := make(map[VkDeviceMemory]bool)
		sparseQueue := sb.getSparseQueueFor(buffer.LastBoundQueue(), buffer.Device(), buffer.Info().QueueFamilyIndices().Range())
		oldFamilyIndex = int(sparseQueue.Family())
		if !buffer.Info().DedicatedAllocationNV().IsNil() {
			for _, bind := range buffer.SparseMemoryBindings().Range() {
				if _, ok := memories[bind.Memory()]; !ok {
					memories[bind.Memory()] = true
					sb.createDeviceMemory(os.DeviceMemories().Get(bind.Memory()), true)
				}
			}
		}

		sb.write(sb.cb.VkQueueBindSparse(
			sparseQueue.VulkanHandle(),
			1,
			sb.MustAllocReadData(
				NewVkBindSparseInfo(
					VkStructureType_VK_STRUCTURE_TYPE_BIND_SPARSE_INFO, // sType
					0, // pNext
					0, // waitSemaphoreCount
					0, // pWaitSemaphores
					1, // bufferBindCount
					NewVkSparseBufferMemoryBindInfoᶜᵖ(sb.MustAllocReadData( // pBufferBinds
						NewVkSparseBufferMemoryBindInfo(
							buffer.VulkanHandle(),                       // buffer
							uint32(buffer.SparseMemoryBindings().Len()), // bindCount
							NewVkSparseMemoryBindᶜᵖ( // pBinds
								sb.MustUnpackReadMap(buffer.SparseMemoryBindings().Range()).Ptr(),
							),
						)).Ptr()),
					0, // imageOpaqueBindCount
					0, // pImageOpaqueBinds
					0, // imageBindCount
					0, // pImageBinds
					0, // signalSemaphoreCount
					0, // pSignalSemaphores
				)).Ptr(),
			VkFence(0),
			VkResult_VK_SUCCESS,
		))
		if sparseResidency || IsFullyBound(0, buffer.Info().Size(), buffer.SparseMemoryBindings()) {
			for _, bind := range buffer.SparseMemoryBindings().Range() {
				size := bind.Size()
				data := sb.s.DeviceMemories().Get(bind.Memory()).Data().Slice(
					uint64(bind.MemoryOffset()),
					uint64(bind.MemoryOffset()+size),
				).MustRead(sb.ctx, nil, sb.oldState, nil)
				contents = append(contents, data...)
				copies = append(copies, NewVkBufferCopy(
					offset,                // srcOffset
					bind.ResourceOffset(), // dstOffset
					size, // size
				))
				offset += size
				offset = (offset + VkDeviceSize(7)) & (^VkDeviceSize(7))
			}
		}
	} else {
		// Otherwise, we have no sparse bindings, we are either non-sparse, or empty.
		if buffer.Memory().IsNil() {
			return
		}

		sb.write(sb.cb.VkBindBufferMemory(
			buffer.Device(),
			buffer.VulkanHandle(),
			buffer.Memory().VulkanHandle(),
			buffer.MemoryOffset(),
			VkResult_VK_SUCCESS,
		))

		size := buffer.Info().Size()
		data := buffer.Memory().Data().Slice(
			uint64(buffer.MemoryOffset()),
			uint64(buffer.MemoryOffset()+size),
		).MustRead(sb.ctx, nil, sb.oldState, nil)
		contents = append(contents, data...)
		copies = append(copies, NewVkBufferCopy(
			offset, // srcOffset
			0,      // dstOffset
			size,   // size
		))
	}

	scratchBuffer, scratchMemory := sb.allocAndFillScratchBuffer(
		sb.s.Devices().Get(buffer.Device()),
		contents,
		VkBufferUsageFlagBits_VK_BUFFER_USAGE_TRANSFER_SRC_BIT)

	commandBuffer, commandPool := sb.getCommandBuffer(queue)

	newFamilyIndex := queue.Family()

	if oldFamilyIndex == -1 {
		oldFamilyIndex = 0
		newFamilyIndex = 0
	}

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		0,
		memory.Nullptr,
		1,
		sb.MustAllocReadData(
			NewVkBufferMemoryBarrier(
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER, // sType
				0, // pNext
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // srcAccessMask
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // dstAccessMask
				uint32(oldFamilyIndex), // srcQueueFamilyIndex
				uint32(newFamilyIndex), // dstQueueFamilyIndex
				scratchBuffer,          // buffer
				0,                      // offset
				VkDeviceSize(len(contents)), // size
			)).Ptr(),
		0,
		memory.Nullptr,
	))

	sb.write(sb.cb.VkCmdCopyBuffer(
		commandBuffer,
		scratchBuffer,
		buffer.VulkanHandle(),
		uint32(len(copies)),
		sb.MustAllocReadData(copies).Ptr(),
	))

	sb.write(sb.cb.VkCmdPipelineBarrier(
		commandBuffer,
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkPipelineStageFlags(VkPipelineStageFlagBits_VK_PIPELINE_STAGE_ALL_COMMANDS_BIT),
		VkDependencyFlags(0),
		0,
		memory.Nullptr,
		1,
		sb.MustAllocReadData(
			NewVkBufferMemoryBarrier(
				VkStructureType_VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER, // sType
				0, // pNext
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // srcAccessMask
				VkAccessFlags((VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT-1)|VkAccessFlagBits_VK_ACCESS_MEMORY_WRITE_BIT), // dstAccessMask
				0, // srcQueueFamilyIndex
				0, // dstQueueFamilyIndex
				buffer.VulkanHandle(), // buffer
				0, // offset
				VkDeviceSize(len(contents)), // size
			)).Ptr(),
		0,
		memory.Nullptr,
	))

	sb.endSubmitAndDestroyCommandBuffer(queue, commandBuffer, commandPool)

	sb.freeScratchBuffer(sb.s.Devices().Get(buffer.Device()), scratchBuffer, scratchMemory)
}

func nextMultipleOf8(v uint64) uint64 {
	return (v + 7) & ^uint64(7)
}

type byteSizeAndExtent struct {
	levelSize             uint64
	alignedLevelSize      uint64
	levelSizeInBuf        uint64
	alignedLevelSizeInBuf uint64
	width                 uint64
	height                uint64
	depth                 uint64
}

func (sb *stateBuilder) levelSize(extent VkExtent3D, format VkFormat, mipLevel uint32, aspect VkImageAspectFlagBits) byteSizeAndExtent {
	elementAndTexelBlockSize, _ :=
		subGetElementAndTexelBlockSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format)
	texelWidth := elementAndTexelBlockSize.TexelBlockSize().Width()
	texelHeight := elementAndTexelBlockSize.TexelBlockSize().Height()

	width, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Width(), mipLevel)
	height, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Height(), mipLevel)
	depth, _ := subGetMipSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, extent.Depth(), mipLevel)
	widthInBlocks, _ := subRoundUpTo(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, width, texelWidth)
	heightInBlocks, _ := subRoundUpTo(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, height, texelHeight)
	elementSize := uint32(0)
	switch aspect {
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_COLOR_BIT:
		elementSize = elementAndTexelBlockSize.ElementSize()
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_DEPTH_BIT:
		elementSize, _ = subGetDepthElementSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format, false)
	case VkImageAspectFlagBits_VK_IMAGE_ASPECT_STENCIL_BIT:
		// Stencil element is always 1 byte wide
		elementSize = uint32(1)
	}
	// The Depth element size might be different when it is in buffer instead of image.
	elementSizeInBuf := elementSize
	if aspect == VkImageAspectFlagBits_VK_IMAGE_ASPECT_DEPTH_BIT {
		elementSizeInBuf, _ = subGetDepthElementSize(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, format, true)
	}

	size := uint64(widthInBlocks) * uint64(heightInBlocks) * uint64(depth) * uint64(elementSize)
	sizeInBuf := uint64(widthInBlocks) * uint64(heightInBlocks) * uint64(depth) * uint64(elementSizeInBuf)

	return byteSizeAndExtent{
		levelSize:             size,
		alignedLevelSize:      nextMultipleOf8(size),
		levelSizeInBuf:        sizeInBuf,
		alignedLevelSizeInBuf: nextMultipleOf8(sizeInBuf),
		width:  uint64(width),
		height: uint64(height),
		depth:  uint64(depth),
	}
}

func (sb *stateBuilder) imageAspectFlagBits(flag VkImageAspectFlags) []VkImageAspectFlagBits {
	bits := []VkImageAspectFlagBits{}
	b, _ := subUnpackImageAspectFlags(sb.ctx, nil, api.CmdNoID, nil, sb.oldState, nil, 0, nil, flag)
	for _, bit := range b.Bits().Range() {
		bits = append(bits, bit)
	}
	return bits
}

// IsFullyBound returns true if the resource range from offset with size is
// fully covered in the bindings.
func IsFullyBound(offset VkDeviceSize, size VkDeviceSize,
	bindings U64ːVkSparseMemoryBindᵐ) bool {
	resourceOffsets := bindings.Keys()

	oneAfterReqRange := -1
	for i := range resourceOffsets {
		if resourceOffsets[i] > uint64(offset+size) {
			oneAfterReqRange = i
			break
		}
	}
	if oneAfterReqRange == -1 || oneAfterReqRange == 0 {
		return false
	}
	i := oneAfterReqRange - 1

	end := offset + size
	for i > 0 && end > offset {
		resOffset := resourceOffsets[i]
		if resOffset+uint64(bindings.Get(resOffset).Size()) >= uint64(end) {
			end = VkDeviceSize(resOffset)
			i--
			continue
		}
		return false
	}

	if end <= offset {
		return true
	}

	if i == 0 {
		resOffset := resourceOffsets[0]
		if resOffset <= uint64(offset) &&
			resOffset+uint64(bindings.Get(resOffset).Size()) >= uint64(end) {
			return true
		}
	}
	return false
}

func (sb *stateBuilder) createImage(img ImageObjectʳ, imgPrimer *imagePrimer) {
	if img.IsSwapchainImage() {
		return
	}

	transDstBit := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_TRANSFER_DST_BIT)
	attBits := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VkImageUsageFlagBits_VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT)
	storageBit := VkImageUsageFlags(VkImageUsageFlagBits_VK_IMAGE_USAGE_STORAGE_BIT)

	primeByBufCopy := (img.Info().Usage() & transDstBit) != 0
	primeByRendering := (!primeByBufCopy) && ((img.Info().Usage() & attBits) != 0)
	primeByImageStore := (!primeByBufCopy) && (!primeByRendering) && ((img.Info().Usage() & storageBit) != 0)

	vkCreateImage(sb, img.Device(), img.Info(), img.VulkanHandle())
	vkGetImageMemoryRequirements(sb, img.Device(), img.VulkanHandle(), img.MemoryRequirements())

	denseBound := !img.BoundMemory().IsNil()
	sparseBound := img.SparseImageMemoryBindings().Len() > 0 ||
		img.OpaqueSparseMemoryBindings().Len() > 0
	sparseBinding :=
		(uint64(img.Info().Flags()) &
			uint64(VkImageCreateFlagBits_VK_IMAGE_CREATE_SPARSE_BINDING_BIT)) != 0
	sparseResidency :=
		sparseBinding &&
			(uint64(img.Info().Flags())&
				uint64(VkImageCreateFlagBits_VK_IMAGE_CREATE_SPARSE_RESIDENCY_BIT)) != 0

	// Dedicated allocation buffer/image must NOT be a sparse binding one.
	// Checking the dedicated allocation info on both the memory and the buffer
	// side, because we've found applications that do miss one of them.
	dedicatedMemoryNV := !img.BoundMemory().IsNil() && (!img.Info().DedicatedAllocationNV().IsNil() || !img.BoundMemory().DedicatedAllocationNV().IsNil())
	// Emit error message to report view if we found one of the dedicate allocation
	// info struct is missing.
	if dedicatedMemoryNV && img.Info().DedicatedAllocationNV().IsNil() {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkImage", uint64(img.VulkanHandle()))
	}
	if dedicatedMemoryNV && img.BoundMemory().DedicatedAllocationNV().IsNil() {
		subVkErrorExpectNVDedicatedlyAllocatedHandle(sb.ctx, nil, api.CmdNoID, nil,
			sb.oldState, GetState(sb.oldState), 0, nil, "VkDeviceMemory", uint64(img.BoundMemory().VulkanHandle()))
	}

	if dedicatedMemoryNV {
		sb.createDeviceMemory(img.BoundMemory(), true)
	}

	if !denseBound && !sparseBound {
		return
	}

	queue := sb.getQueueFor(img.LastBoundQueue(), img.Device(), img.Info().QueueFamilyIndices().Range())
	var sparseQueue QueueObjectʳ
	opaqueRanges := []VkImageSubresourceRange{}

	if img.OpaqueSparseMemoryBindings().Len() > 0 || img.SparseImageMemoryBindings().Len() > 0 {
		// If this img has sparse memory bindings, then we have to set them all
		// now
		if queue.IsNil() {
			return
		}
		sparseQueue = sb.getSparseQueueFor(img.LastBoundQueue(), img.Device(), img.Info().QueueFamilyIndices().Range())
		memories := make(map[VkDeviceMemory]bool)

		nonSparseInfos := []VkSparseImageMemoryBind{}

		for aspect, info := range img.SparseImageMemoryBindings().Range() {
			for layer, layerInfo := range info.Layers().Range() {
				for level, levelInfo := range layerInfo.Levels().Range() {
					for _, block := range levelInfo.Blocks().Range() {
						if !img.Info().DedicatedAllocationNV().IsNil() {
							// If this was a dedicated allocation set it here
							if _, ok := memories[block.Memory()]; !ok {
								memories[block.Memory()] = true
								sb.createDeviceMemory(sb.s.DeviceMemories().Get(block.Memory()), true)
							}
							nonSparseInfos = append(nonSparseInfos, NewVkSparseImageMemoryBind(
								NewVkImageSubresource( // subresource
									VkImageAspectFlags(aspect), // aspectMask
									level, // mipLevel
									layer, // arrayLayer
								),
								block.Offset(),       // offset
								block.Extent(),       // extent
								block.Memory(),       // memory
								block.MemoryOffset(), // memoryOffset
								block.Flags(),        // flags
							))
						}
					}
				}
			}
		}

		sb.write(sb.cb.VkQueueBindSparse(
			sparseQueue.VulkanHandle(),
			1,
			sb.MustAllocReadData(
				NewVkBindSparseInfo(
					VkStructureType_VK_STRUCTURE_TYPE_BIND_SPARSE_INFO, // // sType
					0, // // pNext
					0, // // waitSemaphoreCount
					0, // // pWaitSemaphores
					0, // // bufferBindCount
					0, // // pBufferBinds
					1, // // imageOpaqueBindCount
					NewVkSparseImageOpaqueMemoryBindInfoᶜᵖ(sb.MustAllocReadData( // pImageOpaqueBinds
						NewVkSparseImageOpaqueMemoryBindInfo(
							img.VulkanHandle(),                             // image
							uint32(img.OpaqueSparseMemoryBindings().Len()), // bindCount
							NewVkSparseMemoryBindᶜᵖ( // pBinds
								sb.MustUnpackReadMap(img.OpaqueSparseMemoryBindings().Range()).Ptr(),
							),
						)).Ptr()),
					0, // imageBindCount
					NewVkSparseImageMemoryBindInfoᶜᵖ(sb.MustAllocReadData( // pImageBinds
						NewVkSparseImageMemoryBindInfo(
							img.VulkanHandle(),          // image
							uint32(len(nonSparseInfos)), // bindCount
							NewVkSparseImageMemoryBindᶜᵖ( // pBinds
								sb.MustAllocReadData(nonSparseInfos).Ptr(),
							),
						)).Ptr()),
					0, // signalSemaphoreCount
					0, // pSignalSemaphores
				)).Ptr(),
			VkFence(0),
			VkResult_VK_SUCCESS,
		))

		if sparseResidency {
			isMetadataBound := false
			for _, req := range img.SparseMemoryRequirements().Range() {
				prop := req.FormatProperties()
				if uint64(prop.AspectMask())&uint64(VkImageAspectFlagBits_VK_IMAGE_ASPECT_METADATA_BIT) != 0 {
					isMetadataBound = IsFullyBound(req.ImageMipTailOffset(), req.ImageMipTailSize(), img.OpaqueSparseMemoryBindings())
				}
			}
			if !isMetadataBound {
				// If we have no metadata then the image can have no "real"
				// contents
			} else {
				for _, req := range img.SparseMemoryRequirements().Range() {
					prop := req.FormatProperties()
					if (uint64(prop.Flags()) & uint64(VkSparseImageFormatFlagBits_VK_SPARSE_IMAGE_FORMAT_SINGLE_MIPTAIL_BIT)) != 0 {
						if !IsFullyBound(req.ImageMipTailOffset(), req.ImageMipTailSize(), img.OpaqueSparseMemoryBindings()) {
							continue
						}
						opaqueRanges = append(opaqueRanges, NewVkImageSubresourceRange(
							img.ImageAspect(),                                 // aspectMask
							req.ImageMipTailFirstLod(),                        // baseMipLevel
							img.Info().MipLevels()-req.ImageMipTailFirstLod(), // levelCount
							0, // baseArrayLayer
							img.Info().ArrayLayers(), // layerCount
						))
					} else {
						for i := uint32(0); i < uint32(img.Info().ArrayLayers()); i++ {
							offset := req.ImageMipTailOffset() + VkDeviceSize(i)*req.ImageMipTailStride()
							if !IsFullyBound(offset, req.ImageMipTailSize(), img.OpaqueSparseMemoryBindings()) {
								continue
							}
							opaqueRanges = append(opaqueRanges, NewVkImageSubresourceRange(
								img.ImageAspect(),                                 // aspectMask
								req.ImageMipTailFirstLod(),                        // baseMipLevel
								img.Info().MipLevels()-req.ImageMipTailFirstLod(), // levelCount
								i, // baseArrayLayer
								1, // layerCount
							))
						}
					}
				}
			}
		} else {
			if IsFullyBound(0, img.MemoryRequirements().Size(), img.OpaqueSparseMemoryBindings()) {
				opaqueRanges = append(opaqueRanges, NewVkImageSubresourceRange(
					img.ImageAspect(), // aspectMask
					0,                 // baseMipLevel
					img.Info().MipLevels(), // levelCount
					0, // baseArrayLayer
					img.Info().ArrayLayers(), // layerCount
				))
			}
		}
	} else {
		// Otherwise, we have no sparse bindings, we are either non-sparse, or empty.
		if img.BoundMemory().IsNil() {
			return
		}

		opaqueRanges = append(opaqueRanges, NewVkImageSubresourceRange(
			img.ImageAspect(), // aspectMask
			0,                 // baseMipLevel
			img.Info().MipLevels(), // levelCount
			0, // baseArrayLayer
			img.Info().ArrayLayers(), // layerCount
		))
		vkBindImageMemory(sb, img.Device(), img.VulkanHandle(),
			img.BoundMemory().VulkanHandle(), img.BoundMemoryOffset())
	}

	// We won't have to handle UNDEFINED.
	if img.Info().Layout() == VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED {
		return
	}
	// We don't currently prime the data in any of these formats.
	if img.Info().Samples() != VkSampleCountFlagBits_VK_SAMPLE_COUNT_1_BIT {
		sb.transitionImage(img, VkImageLayout_VK_IMAGE_LAYOUT_UNDEFINED, img.Info().Layout(), sparseQueue, queue)
		log.E(sb.ctx, "[Priming the data of image: %v] priming data for MS images not implemented", img.VulkanHandle())
		return
	}
	if img.LastBoundQueue().IsNil() {
		log.W(sb.ctx, "[Priming the data of image: %v] image has never been used on any queue, using arbitrary queue for the priming commands", img.VulkanHandle())
	}
	// We have to handle the above cases at some point.
	var err error
	if primeByBufCopy {
		err = imgPrimer.primeByBufferCopy(img, opaqueRanges, queue, sparseQueue)
	} else if primeByRendering {
		err = imgPrimer.primeByRendering(img, opaqueRanges, queue, sparseQueue)
	} else if primeByImageStore {
		err = imgPrimer.primeByImageStore(img, opaqueRanges, queue, sparseQueue)
	}
	if err != nil {
		log.E(sb.ctx, "[Priming the data of image: %v] %v", img.VulkanHandle, err)
	}
	return
}

func (sb *stateBuilder) createSampler(smp SamplerObjectʳ) {
	sb.write(sb.cb.VkCreateSampler(
		smp.Device(),
		sb.MustAllocReadData(NewVkSamplerCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO, // sType
			0,                             // pNext
			0,                             // flags
			smp.MagFilter(),               // magFilter
			smp.MinFilter(),               // minFilter
			smp.MipMapMode(),              // mipmapMode
			smp.AddressModeU(),            // addressModeU
			smp.AddressModeV(),            // addressModeV
			smp.AddressModeW(),            // addressModeW
			smp.MipLodBias(),              // mipLodBias
			smp.AnisotropyEnable(),        // anisotropyEnable
			smp.MaxAnisotropy(),           // maxAnisotropy
			smp.CompareEnable(),           // compareEnable
			smp.CompareOp(),               // compareOp
			smp.MinLod(),                  // minLod
			smp.MaxLod(),                  // maxLod
			smp.BorderColor(),             // borderColor
			smp.UnnormalizedCoordinates(), // unnormalizedCoordinates
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(smp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createFence(fnc FenceObjectʳ) {
	flags := VkFenceCreateFlags(0)
	if fnc.Signaled() {
		flags = VkFenceCreateFlags(VkFenceCreateFlagBits_VK_FENCE_CREATE_SIGNALED_BIT)
	}
	sb.write(sb.cb.VkCreateFence(
		fnc.Device(),
		sb.MustAllocReadData(NewVkFenceCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_FENCE_CREATE_INFO, // sType
			0,     // pNext
			flags, // flags
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(fnc.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createSemaphore(sem SemaphoreObjectʳ) {
	sb.write(sb.cb.VkCreateSemaphore(
		sem.Device(),
		sb.MustAllocReadData(NewVkSemaphoreCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO, // sType
			0, // pNext
			0, // flags
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(sem.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if !sem.Signaled() {
		return
	}

	queue := sem.LastQueue()
	if !sb.s.Queues().Contains(queue) {
		// find a queue with the same device
		for _, q := range sb.s.Queues().Range() {
			if q.Device() == sem.Device() {
				queue = q.VulkanHandle()
			}
		}
	}

	sb.write(sb.cb.VkQueueSubmit(
		queue,
		1,
		sb.MustAllocReadData(NewVkSubmitInfo(
			VkStructureType_VK_STRUCTURE_TYPE_SUBMIT_INFO, // sType
			0, // pNext
			0, // waitSemaphoreCount
			0, // pWaitSemaphores
			0, // pWaitDstStageMask
			0, // commandBufferCount
			0, // pCommandBuffers
			1, // signalSemaphoreCount
			NewVkSemaphoreᶜᵖ(sb.MustAllocReadData(sem.VulkanHandle()).Ptr()), // pSignalSemaphores
		)).Ptr(),
		VkFence(0),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createEvent(evt EventObjectʳ) {
	sb.write(sb.cb.VkCreateEvent(
		evt.Device(),
		sb.MustAllocReadData(NewVkEventCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_EVENT_CREATE_INFO, // sType
			0, // pNext
			0, // flags
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(evt.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if evt.Signaled() {
		sb.write(sb.cb.VkSetEvent(
			evt.Device(),
			evt.VulkanHandle(),
			VkResult_VK_SUCCESS,
		))
	}
}

func (sb *stateBuilder) createCommandPool(cp CommandPoolObjectʳ) {
	sb.write(sb.cb.VkCreateCommandPool(
		cp.Device(),
		sb.MustAllocReadData(NewVkCommandPoolCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO, // sType
			0,                     // pNext
			cp.Flags(),            // flags
			cp.QueueFamilyIndex(), // queueFamilyIndex
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(cp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPipelineCache(pc PipelineCacheObjectʳ) {
	sb.write(sb.cb.VkCreatePipelineCache(
		pc.Device(),
		sb.MustAllocReadData(NewVkPipelineCacheCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_CACHE_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			0, // initialDataSize
			0, // pInitialData
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(pc.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createDescriptorSetLayout(dsl DescriptorSetLayoutObjectʳ) {
	bindings := []VkDescriptorSetLayoutBinding{}
	for _, k := range dsl.Bindings().Keys() {
		b := dsl.Bindings().Get(k)
		smp := NewVkSamplerᶜᵖ(memory.Nullptr)
		if b.ImmutableSamplers().Len() > 0 {
			immutableSamplers := []VkSampler{}
			for _, kk := range b.ImmutableSamplers().Keys() {
				immutableSamplers = append(immutableSamplers, b.ImmutableSamplers().Get(kk).VulkanHandle())
			}
			allocateResult := sb.newState.AllocDataOrPanic(sb.ctx, immutableSamplers)
			sb.readMemories = append(sb.readMemories, &allocateResult)
			smp = NewVkSamplerᶜᵖ(allocateResult.Ptr())
		}

		bindings = append(bindings, NewVkDescriptorSetLayoutBinding(
			k,          // binding
			b.Type(),   // descriptorType
			b.Count(),  // descriptorCount
			b.Stages(), // stageFlags
			smp,        // pImmutableSamplers
		))
	}

	sb.write(sb.cb.VkCreateDescriptorSetLayout(
		dsl.Device(),
		sb.MustAllocReadData(NewVkDescriptorSetLayoutCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			uint32(len(bindings)), // bindingCount
			NewVkDescriptorSetLayoutBindingᶜᵖ( // pBindings
				sb.MustAllocReadData(bindings).Ptr(),
			),
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(dsl.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createPipelineLayout(pl PipelineLayoutObjectʳ) {
	descriptorSets := []VkDescriptorSetLayout{}
	for _, k := range pl.SetLayouts().Keys() {
		descriptorSets = append(descriptorSets, pl.SetLayouts().Get(k).VulkanHandle())
	}

	sb.write(sb.cb.VkCreatePipelineLayout(
		pl.Device(),
		sb.MustAllocReadData(NewVkPipelineLayoutCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			uint32(len(descriptorSets)), // setLayoutCount
			NewVkDescriptorSetLayoutᶜᵖ( // pSetLayouts
				sb.MustAllocReadData(descriptorSets).Ptr(),
			),
			uint32(pl.PushConstantRanges().Len()),                                                 // pushConstantRangeCount
			NewVkPushConstantRangeᶜᵖ(sb.MustUnpackReadMap(pl.PushConstantRanges().Range()).Ptr()), // pPushConstantRanges
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(pl.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createRenderPass(rp RenderPassObjectʳ) {
	subpassDescriptions := []VkSubpassDescription{}
	for _, k := range rp.SubpassDescriptions().Keys() {
		sd := rp.SubpassDescriptions().Get(k)
		depthStencil := NewVkAttachmentReferenceᶜᵖ(memory.Nullptr)
		if !sd.DepthStencilAttachment().IsNil() {
			depthStencil = NewVkAttachmentReferenceᶜᵖ(sb.MustAllocReadData(sd.DepthStencilAttachment().Get()).Ptr())
		}
		resolveAttachments := NewVkAttachmentReferenceᶜᵖ(memory.Nullptr)
		if sd.ResolveAttachments().Len() > 0 {
			resolveAttachments = NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(sd.ResolveAttachments().Range()).Ptr())
		}

		subpassDescriptions = append(subpassDescriptions, NewVkSubpassDescription(
			sd.Flags(),                                                                            // flags
			sd.PipelineBindPoint(),                                                                // pipelineBindPoint
			uint32(sd.InputAttachments().Len()),                                                   // inputAttachmentCount
			NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(sd.InputAttachments().Range()).Ptr()), // pInputAttachments
			uint32(sd.ColorAttachments().Len()),                                                   // colorAttachmentCount
			NewVkAttachmentReferenceᶜᵖ(sb.MustUnpackReadMap(sd.ColorAttachments().Range()).Ptr()), // pColorAttachments
			resolveAttachments,                                                     // pResolveAttachments
			depthStencil,                                                           // pDepthStencilAttachment
			uint32(sd.PreserveAttachments().Len()),                                 // preserveAttachmentCount
			NewU32ᶜᵖ(sb.MustUnpackReadMap(sd.PreserveAttachments().Range()).Ptr()), // pPreserveAttachments
		))
	}

	sb.write(sb.cb.VkCreateRenderPass(
		rp.Device(),
		sb.MustAllocReadData(NewVkRenderPassCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			uint32(rp.AttachmentDescriptions().Len()),                                                     // attachmentCount
			NewVkAttachmentDescriptionᶜᵖ(sb.MustUnpackReadMap(rp.AttachmentDescriptions().Range()).Ptr()), // pAttachments
			uint32(len(subpassDescriptions)),                                                              // subpassCount
			NewVkSubpassDescriptionᶜᵖ(sb.MustAllocReadData(subpassDescriptions).Ptr()),                    // pSubpasses
			uint32(rp.SubpassDependencies().Len()),                                                        // dependencyCount
			NewVkSubpassDependencyᶜᵖ(sb.MustUnpackReadMap(rp.SubpassDependencies().Range()).Ptr()),        // pDependencies
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(rp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createShaderModule(sm ShaderModuleObjectʳ) {
	words := sm.Words().MustRead(sb.ctx, nil, sb.oldState, nil)

	sb.write(sb.cb.VkCreateShaderModule(
		sm.Device(),
		sb.MustAllocReadData(NewVkShaderModuleCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			memory.Size(len(words))*4,                   // codeSize
			NewU32ᶜᵖ(sb.MustAllocReadData(words).Ptr()), // pCode
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(sm.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createComputePipeline(cp ComputePipelineObjectʳ) {
	cache := VkPipelineCache(0)
	if !cp.PipelineCache().IsNil() {
		cache = cp.PipelineCache().VulkanHandle()
	}

	basePipeline := VkPipeline(0)
	if cp.BasePipeline() != 0 {
		if GetState(sb.newState).ComputePipelines().Contains(cp.BasePipeline()) {
			basePipeline = cp.BasePipeline()
		}
	}

	var temporaryShaderModule ShaderModuleObjectʳ

	if !GetState(sb.newState).ShaderModules().Contains(cp.Stage().Module().VulkanHandle()) {
		// This is a previously deleted shader module, recreate it, then clear it
		sb.createShaderModule(cp.Stage().Module())
		temporaryShaderModule = cp.Stage().Module()
	}

	specializationInfo := NewVkSpecializationInfoᶜᵖ(memory.Nullptr)
	if !cp.Stage().Specialization().IsNil() {
		data := cp.Stage().Specialization().Data().MustRead(sb.ctx, nil, sb.oldState, nil)
		specializationInfo = NewVkSpecializationInfoᶜᵖ(sb.MustAllocReadData(NewVkSpecializationInfo(
			uint32(cp.Stage().Specialization().Specializations().Len()),                                                      // mapEntryCount
			NewVkSpecializationMapEntryᶜᵖ(sb.MustUnpackReadMap(cp.Stage().Specialization().Specializations().Range()).Ptr()), // pMapEntries
			memory.Size(len(data)),                      // dataSize
			NewVoidᶜᵖ(sb.MustAllocReadData(data).Ptr()), // pData
		)).Ptr())
	}

	sb.write(sb.cb.VkCreateComputePipelines(
		cp.Device(),
		cache,
		1,
		sb.MustAllocReadData(NewVkComputePipelineCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO, // sType
			0,          // pNext
			cp.Flags(), // flags
			NewVkPipelineShaderStageCreateInfo( // stage
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, // sType
				0,                                                              // pNext
				0,                                                              // flags
				cp.Stage().Stage(),                                             // stage
				cp.Stage().Module().VulkanHandle(),                             // module
				NewCharᶜᵖ(sb.MustAllocReadData(cp.Stage().EntryPoint()).Ptr()), // pName
				specializationInfo,                                             // pSpecializationInfo
			),
			cp.PipelineLayout().VulkanHandle(), // layout
			basePipeline,                       // basePipelineHandle
			-1,                                 // basePipelineIndex
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(cp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if !temporaryShaderModule.IsNil() {
		sb.write(sb.cb.VkDestroyShaderModule(
			temporaryShaderModule.Device(),
			temporaryShaderModule.VulkanHandle(),
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createGraphicsPipeline(gp GraphicsPipelineObjectʳ) {
	cache := VkPipelineCache(0)
	if !gp.PipelineCache().IsNil() {
		cache = gp.PipelineCache().VulkanHandle()
	}

	basePipeline := VkPipeline(0)
	if gp.BasePipeline() != 0 {
		if GetState(sb.newState).GraphicsPipelines().Contains(gp.BasePipeline()) {
			basePipeline = gp.BasePipeline()
		}
	}

	stagesInOrder := gp.Stages().Keys()

	temporaryShaderModules := []ShaderModuleObjectʳ{}
	stages := []VkPipelineShaderStageCreateInfo{}
	for _, ss := range stagesInOrder {
		s := gp.Stages().Get(ss)
		if !GetState(sb.newState).ShaderModules().Contains(s.Module().VulkanHandle()) {
			// create temporary shader modules for the pipeline to be created.
			sb.createShaderModule(s.Module())
			temporaryShaderModules = append(temporaryShaderModules, s.Module())
		}
	}

	var temporaryPipelineLayout PipelineLayoutObjectʳ
	if !GetState(sb.newState).PipelineLayouts().Contains(gp.Layout().VulkanHandle()) {
		// create temporary pipeline layout for the pipeline to be created.
		sb.createPipelineLayout(gp.Layout())
		temporaryPipelineLayout = GetState(sb.newState).PipelineLayouts().Get(gp.Layout().VulkanHandle())
	}

	var temporaryRenderPass RenderPassObjectʳ
	if !GetState(sb.newState).RenderPasses().Contains(gp.RenderPass().VulkanHandle()) {
		// create temporary render pass for the pipeline to be created.
		sb.createRenderPass(gp.RenderPass())
		temporaryRenderPass = GetState(sb.newState).RenderPasses().Get(gp.RenderPass().VulkanHandle())
	}

	// DO NOT! coalesce the prevous calls with this one. createShaderModule()
	// makes calls which means pending read/write observations will get
	// shunted off with it instead of on the VkCreateGraphicsPipelines call
	for _, ss := range stagesInOrder {
		s := gp.Stages().Get(ss)
		specializationInfo := NewVkSpecializationInfoᶜᵖ(memory.Nullptr)
		if !s.Specialization().IsNil() {
			data := s.Specialization().Data().MustRead(sb.ctx, nil, sb.oldState, nil)
			specializationInfo = NewVkSpecializationInfoᶜᵖ(sb.MustAllocReadData(
				NewVkSpecializationInfo(
					uint32(s.Specialization().Specializations().Len()),                                                      // mapEntryCount
					NewVkSpecializationMapEntryᶜᵖ(sb.MustUnpackReadMap(s.Specialization().Specializations().Range()).Ptr()), // pMapEntries
					memory.Size(len(data)),                      // dataSize
					NewVoidᶜᵖ(sb.MustAllocReadData(data).Ptr()), // pData
				)).Ptr())
		}
		stages = append(stages, NewVkPipelineShaderStageCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, // sType
			0,                                                     // pNext
			0,                                                     // flags
			s.Stage(),                                             // stage
			s.Module().VulkanHandle(),                             // module
			NewCharᶜᵖ(sb.MustAllocReadData(s.EntryPoint()).Ptr()), // pName
			specializationInfo,                                    // pSpecializationInfo
		))
	}

	tessellationState := NewVkPipelineTessellationStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.TessellationState().IsNil() {
		tessellationState = NewVkPipelineTessellationStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineTessellationStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_TESSELLATION_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				gp.TessellationState().PatchControlPoints(), // patchControlPoints
			)).Ptr())
	}

	viewportState := NewVkPipelineViewportStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.ViewportState().IsNil() {
		viewports := NewVkViewportᶜᵖ(memory.Nullptr)
		if gp.ViewportState().Viewports().Len() > 0 {
			viewports = NewVkViewportᶜᵖ(sb.MustUnpackReadMap(gp.ViewportState().Viewports().Range()).Ptr())
		}
		scissors := NewVkRect2Dᶜᵖ(memory.Nullptr)
		if gp.ViewportState().Scissors().Len() > 0 {
			scissors = NewVkRect2Dᶜᵖ(sb.MustUnpackReadMap(gp.ViewportState().Scissors().Range()).Ptr())
		}

		viewportState = NewVkPipelineViewportStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineViewportStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				gp.ViewportState().ViewportCount(), // viewportCount
				viewports,                          // pViewports
				gp.ViewportState().ScissorCount(),  // scissorCount
				scissors, // pScissors
			)).Ptr())
	}

	multisampleState := NewVkPipelineMultisampleStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.MultisampleState().IsNil() {
		sampleMask := NewVkSampleMaskᶜᵖ(memory.Nullptr)
		if gp.MultisampleState().SampleMask().Len() > 0 {
			sampleMask = NewVkSampleMaskᶜᵖ(sb.MustUnpackReadMap(gp.MultisampleState().SampleMask().Range()).Ptr())
		}
		multisampleState = NewVkPipelineMultisampleStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineMultisampleStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				gp.MultisampleState().RasterizationSamples(), // rasterizationSamples
				gp.MultisampleState().SampleShadingEnable(),  // sampleShadingEnable
				gp.MultisampleState().MinSampleShading(),     // minSampleShading
				sampleMask, // pSampleMask
				gp.MultisampleState().AlphaToCoverageEnable(), // alphaToCoverageEnable
				gp.MultisampleState().AlphaToOneEnable(),      // alphaToOneEnable
			)).Ptr())
	}

	depthState := NewVkPipelineDepthStencilStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.DepthState().IsNil() {
		depthState = NewVkPipelineDepthStencilStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineDepthStencilStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				gp.DepthState().DepthTestEnable(),       // depthTestEnable
				gp.DepthState().DepthWriteEnable(),      // depthWriteEnable
				gp.DepthState().DepthCompareOp(),        // depthCompareOp
				gp.DepthState().DepthBoundsTestEnable(), // depthBoundsTestEnable
				gp.DepthState().StencilTestEnable(),     // stencilTestEnable
				gp.DepthState().Front(),                 // front
				gp.DepthState().Back(),                  // back
				gp.DepthState().MinDepthBounds(),        // minDepthBounds
				gp.DepthState().MaxDepthBounds(),        // maxDepthBounds
			)).Ptr())
	}

	colorBlendState := NewVkPipelineColorBlendStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.ColorBlendState().IsNil() {
		colorblendAttachments := NewVkPipelineColorBlendAttachmentStateᶜᵖ(memory.Nullptr)
		if gp.ColorBlendState().Attachments().Len() > 0 {
			colorblendAttachments = NewVkPipelineColorBlendAttachmentStateᶜᵖ(sb.MustUnpackReadMap(gp.ColorBlendState().Attachments().Range()).Ptr())
		}
		colorBlendState = NewVkPipelineColorBlendStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineColorBlendStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				gp.ColorBlendState().LogicOpEnable(),             // logicOpEnable
				gp.ColorBlendState().LogicOp(),                   // logicOp
				uint32(gp.ColorBlendState().Attachments().Len()), // attachmentCount
				colorblendAttachments,                            // pAttachments
				gp.ColorBlendState().BlendConstants(),            // blendConstants
			)).Ptr())
	}

	dynamicState := NewVkPipelineDynamicStateCreateInfoᶜᵖ(memory.Nullptr)
	if !gp.DynamicState().IsNil() {
		dynamicStates := NewVkDynamicStateᶜᵖ(memory.Nullptr)
		if gp.DynamicState().DynamicStates().Len() > 0 {
			dynamicStates = NewVkDynamicStateᶜᵖ(sb.MustUnpackReadMap(gp.DynamicState().DynamicStates().Range()).Ptr())
		}
		dynamicState = NewVkPipelineDynamicStateCreateInfoᶜᵖ(sb.MustAllocReadData(
			NewVkPipelineDynamicStateCreateInfo(
				VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO, // sType
				0, // pNext
				0, // flags
				uint32(gp.DynamicState().DynamicStates().Len()), // dynamicStateCount
				dynamicStates,                                   // pDynamicStates
			)).Ptr())
	}

	sb.write(sb.cb.VkCreateGraphicsPipelines(
		gp.Device(),
		cache,
		1,
		sb.MustAllocReadData(NewVkGraphicsPipelineCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO, // sType
			0,                   // pNext
			gp.Flags(),          // flags
			uint32(len(stages)), // stageCount
			NewVkPipelineShaderStageCreateInfoᶜᵖ(sb.MustAllocReadData(stages).Ptr()), // pStages
			NewVkPipelineVertexInputStateCreateInfoᶜᵖ(sb.MustAllocReadData( // pVertexInputState
				NewVkPipelineVertexInputStateCreateInfo(
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO, // sType
					0, // pNext
					0, // flags
					uint32(gp.VertexInputState().BindingDescriptions().Len()),                                                                 // vertexBindingDescriptionCount
					NewVkVertexInputBindingDescriptionᶜᵖ(sb.MustUnpackReadMap(gp.VertexInputState().BindingDescriptions().Range()).Ptr()),     // pVertexBindingDescriptions
					uint32(gp.VertexInputState().AttributeDescriptions().Len()),                                                               // vertexAttributeDescriptionCount
					NewVkVertexInputAttributeDescriptionᶜᵖ(sb.MustUnpackReadMap(gp.VertexInputState().AttributeDescriptions().Range()).Ptr()), // pVertexAttributeDescriptions
				)).Ptr()),
			NewVkPipelineInputAssemblyStateCreateInfoᶜᵖ(sb.MustAllocReadData( // pInputAssemblyState
				NewVkPipelineInputAssemblyStateCreateInfo(
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO, // sType
					0, // pNext
					0, // flags
					gp.InputAssemblyState().Topology(),               // topology
					gp.InputAssemblyState().PrimitiveRestartEnable(), // primitiveRestartEnable
				)).Ptr()),
			tessellationState, // pTessellationState
			viewportState,     // pViewportState
			NewVkPipelineRasterizationStateCreateInfoᶜᵖ(sb.MustAllocReadData( // pRasterizationState
				NewVkPipelineRasterizationStateCreateInfo(
					VkStructureType_VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO, // sType
					0, // pNext
					0, // flags
					gp.RasterizationState().DepthClampEnable(),        // depthClampEnable
					gp.RasterizationState().RasterizerDiscardEnable(), // rasterizerDiscardEnable
					gp.RasterizationState().PolygonMode(),             // polygonMode
					gp.RasterizationState().CullMode(),                // cullMode
					gp.RasterizationState().FrontFace(),               // frontFace
					gp.RasterizationState().DepthBiasEnable(),         // depthBiasEnable
					gp.RasterizationState().DepthBiasConstantFactor(), // depthBiasConstantFactor
					gp.RasterizationState().DepthBiasClamp(),          // depthBiasClamp
					gp.RasterizationState().DepthBiasSlopeFactor(),    // depthBiasSlopeFactor
					gp.RasterizationState().LineWidth(),               // lineWidth
				)).Ptr()),
			multisampleState,               // pMultisampleState
			depthState,                     // pDepthStencilState
			colorBlendState,                // pColorBlendState
			dynamicState,                   // pDynamicState
			gp.Layout().VulkanHandle(),     // layout
			gp.RenderPass().VulkanHandle(), // renderPass
			gp.Subpass(),                   // subpass
			basePipeline,                   // basePipelineHandle
			-1,                             // basePipelineIndex
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(gp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	for _, m := range temporaryShaderModules {
		sb.write(sb.cb.VkDestroyShaderModule(
			m.Device(),
			m.VulkanHandle(),
			memory.Nullptr,
		))
	}

	if !temporaryRenderPass.IsNil() {
		sb.write(sb.cb.VkDestroyRenderPass(
			temporaryRenderPass.Device(),
			temporaryRenderPass.VulkanHandle(),
			memory.Nullptr,
		))
	}

	if !temporaryPipelineLayout.IsNil() {
		sb.write(sb.cb.VkDestroyPipelineLayout(
			temporaryPipelineLayout.Device(),
			temporaryPipelineLayout.VulkanHandle(),
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createImageView(iv ImageViewObjectʳ) {
	if !GetState(sb.newState).Images().Contains(iv.Image().VulkanHandle()) {
		// If the image that this image view points to has been deleted,
		// then don't even re-create the image view
		return
	}

	sb.write(sb.cb.VkCreateImageView(
		iv.Device(),
		sb.MustAllocReadData(NewVkImageViewCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			iv.Image().VulkanHandle(), // image
			iv.Type(),                 // viewType
			iv.Fmt(),                  // format
			iv.Components(),           // components
			iv.SubresourceRange(),     // subresourceRange
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(iv.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createBufferView(bv BufferViewObjectʳ) {
	if !GetState(sb.newState).Buffers().Contains(bv.Buffer().VulkanHandle()) {
		// If the image that this image view points to has been deleted,
		// then don't even re-create the image view
		return
	}

	sb.write(sb.cb.VkCreateBufferView(
		bv.Device(),
		sb.MustAllocReadData(NewVkBufferViewCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_BUFFER_VIEW_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			bv.Buffer().VulkanHandle(), // buffer
			bv.Fmt(),                   // format
			bv.Offset(),                // offset
			bv.Range(),                 // range
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(bv.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createDescriptorPool(dp DescriptorPoolObjectʳ) {
	sb.write(sb.cb.VkCreateDescriptorPool(
		dp.Device(),
		sb.MustAllocReadData(NewVkDescriptorPoolCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, // sType
			0,                                                                         // pNext
			dp.Flags(),                                                                // flags
			dp.MaxSets(),                                                              // maxSets
			uint32(dp.Sizes().Len()),                                                  // poolSizeCount
			NewVkDescriptorPoolSizeᶜᵖ(sb.MustUnpackReadMap(dp.Sizes().Range()).Ptr()), // pPoolSizes
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(dp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))
}

func (sb *stateBuilder) createFramebuffer(fb FramebufferObjectʳ) {
	var temporaryRenderPass RenderPassObjectʳ
	if !GetState(sb.newState).RenderPasses().Contains(fb.RenderPass().VulkanHandle()) {
		sb.createRenderPass(fb.RenderPass())
		temporaryRenderPass = GetState(sb.newState).RenderPasses().Get(fb.RenderPass().VulkanHandle())
	}

	imageViews := []VkImageView{}
	for _, v := range fb.ImageAttachments().Keys() {
		imageViews = append(imageViews, fb.ImageAttachments().Get(v).VulkanHandle())
	}

	sb.write(sb.cb.VkCreateFramebuffer(
		fb.Device(),
		sb.MustAllocReadData(NewVkFramebufferCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO, // sType
			0, // pNext
			0, // flags
			fb.RenderPass().VulkanHandle(),                           // renderPass
			uint32(len(imageViews)),                                  // attachmentCount
			NewVkImageViewᶜᵖ(sb.MustAllocReadData(imageViews).Ptr()), // pAttachments
			fb.Width(),  // width
			fb.Height(), // height
			fb.Layers(), // layers
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(fb.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if !temporaryRenderPass.IsNil() {
		sb.write(sb.cb.VkDestroyRenderPass(
			temporaryRenderPass.Device(),
			temporaryRenderPass.VulkanHandle(),
			memory.Nullptr,
		))
	}
}

func (sb *stateBuilder) createDescriptorSet(ds DescriptorSetObjectʳ) {
	ns := GetState(sb.newState)
	if !ns.DescriptorPools().Contains(ds.DescriptorPool()) {
		return
	}
	sb.write(sb.cb.VkAllocateDescriptorSets(
		ds.Device(),
		sb.MustAllocReadData(NewVkDescriptorSetAllocateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, // sType
			0,                   // pNext
			ds.DescriptorPool(), // descriptorPool
			1,                   // descriptorSetCount
			NewVkDescriptorSetLayoutᶜᵖ(sb.MustAllocReadData(ds.Layout().VulkanHandle()).Ptr()), // pSetLayouts
		)).Ptr(),
		sb.MustAllocWriteData(ds.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	writes := []VkWriteDescriptorSet{}
	for _, k := range ds.Bindings().Keys() {
		binding := ds.Bindings().Get(k)
		switch binding.BindingType() {
		case VkDescriptorType_VK_DESCRIPTOR_TYPE_SAMPLER,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_IMAGE,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_INPUT_ATTACHMENT:

			numImages := uint32(binding.ImageBinding().Len())
			for i := uint32(0); i < numImages; i++ {
				im := binding.ImageBinding().Get(i)
				if im.Sampler() == 0 && im.ImageView() == 0 {
					continue
				}
				if binding.BindingType() == VkDescriptorType_VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER &&
					(im.Sampler() == 0 || im.ImageView() == 0) {
					continue
				}
				if im.Sampler() != 0 && !ns.Samplers().Contains(im.Sampler()) {
					log.W(sb.ctx, "Sampler %v is invalid, this descriptor[%v] will remain empty", im.Sampler(), ds.VulkanHandle())
					continue
				}
				if im.ImageView() != 0 && !ns.ImageViews().Contains(im.ImageView()) {
					log.W(sb.ctx, "ImageView %v is invalid, this descriptor[%v] will remain empty", im.Sampler(), ds.VulkanHandle())
					continue
				}

				writes = append(writes, NewVkWriteDescriptorSet(
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, // sType
					0,                 // pNext
					ds.VulkanHandle(), // dstSet
					k,                 // dstBinding
					i,                 // dstArrayElement
					1,                 // descriptorCount
					binding.BindingType(),                                            // descriptorType
					NewVkDescriptorImageInfoᶜᵖ(sb.MustAllocReadData(im.Get()).Ptr()), // pImageInfo
					0, // pBufferInfo
					0, // pTexelBufferView
				))
			}

		case VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_BUFFER,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_BUFFER_DYNAMIC:
			numBuffers := uint32(binding.BufferBinding().Len())
			for i := uint32(0); i < numBuffers; i++ {
				buff := binding.BufferBinding().Get(i)
				if buff.Buffer() == 0 {
					continue
				}
				if buff.Buffer() != 0 && !ns.Buffers().Contains(buff.Buffer()) {
					log.W(sb.ctx, "Buffer %v is invalid, this descriptor[%v] will remain empty", buff.Buffer(), ds.VulkanHandle())
					continue
				}
				writes = append(writes, NewVkWriteDescriptorSet(
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, // sType
					0,                 // pNext
					ds.VulkanHandle(), // dstSet
					k,                 // dstBinding
					i,                 // dstArrayElement
					1,                 // descriptorCount
					binding.BindingType(), // descriptorType
					0, // pImageInfo
					NewVkDescriptorBufferInfoᶜᵖ(sb.MustAllocReadData(buff.Get()).Ptr()), // pBufferInfo
					0, // pTexelBufferView
				))
			}

		case VkDescriptorType_VK_DESCRIPTOR_TYPE_UNIFORM_TEXEL_BUFFER,
			VkDescriptorType_VK_DESCRIPTOR_TYPE_STORAGE_TEXEL_BUFFER:
			numBuffers := uint32(binding.BufferViewBindings().Len())
			for i := uint32(0); i < numBuffers; i++ {
				bv := binding.BufferViewBindings().Get(i)
				if bv == 0 {
					continue
				}
				if bv != 0 && !ns.BufferViews().Contains(bv) {
					log.W(sb.ctx, "BufferView %v is invalid, this descriptor[%v] will remain empty", bv, ds.VulkanHandle())
					continue
				}
				writes = append(writes, NewVkWriteDescriptorSet(
					VkStructureType_VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, // sType
					0,                 // pNext
					ds.VulkanHandle(), // dstSet
					k,                 // dstBinding
					i,                 // dstArrayElement
					1,                 // descriptorCount
					binding.BindingType(), // descriptorType
					0, // pImageInfo
					0, // pBufferInfo
					NewVkBufferViewᶜᵖ(sb.MustAllocReadData(bv).Ptr()), // pTexelBufferView
				))
			}
		}
	}
	sb.write(sb.cb.VkUpdateDescriptorSets(
		ds.Device(),
		uint32(len(writes)),
		sb.MustAllocReadData(writes).Ptr(),
		0,
		memory.Nullptr,
	))
}

func (sb *stateBuilder) createQueryPool(qp QueryPoolObjectʳ) {
	sb.write(sb.cb.VkCreateQueryPool(
		qp.Device(),
		sb.MustAllocReadData(NewVkQueryPoolCreateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO, // sType
			0,                       // pNext
			0,                       // flags
			qp.QueryType(),          // queryType
			qp.QueryCount(),         // queryCount
			qp.PipelineStatistics(), // pipelineStatistics
		)).Ptr(),
		memory.Nullptr,
		sb.MustAllocWriteData(qp.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	anyActive := false
	for _, k := range qp.Status().Range() {
		if k != QueryStatus_QUERY_STATUS_INACTIVE {
			anyActive = true
			break
		}
	}
	if !anyActive {
		return
	}
	queue := sb.getQueueFor(NilQueueObjectʳ, qp.Device(), nil)

	commandBuffer, commandPool := sb.getCommandBuffer(queue)
	for i := uint32(0); i < qp.QueryCount(); i++ {
		if qp.Status().Get(i) != QueryStatus_QUERY_STATUS_INACTIVE {
			sb.write(sb.cb.VkCmdBeginQuery(
				commandBuffer,
				qp.VulkanHandle(),
				i,
				VkQueryControlFlags(0)))
		}
		if qp.Status().Get(i) == QueryStatus_QUERY_STATUS_COMPLETE {
			sb.write(sb.cb.VkCmdEndQuery(
				commandBuffer,
				qp.VulkanHandle(),
				i))
		}
	}

	sb.endSubmitAndDestroyCommandBuffer(queue, commandBuffer, commandPool)
}

func (sb *stateBuilder) createCommandBuffer(cb CommandBufferObjectʳ, level VkCommandBufferLevel) {
	if cb.Level() != level {
		return
	}

	sb.write(sb.cb.VkAllocateCommandBuffers(
		cb.Device(),
		sb.MustAllocReadData(NewVkCommandBufferAllocateInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO, // sType
			0,          // pNext
			cb.Pool(),  // commandPool
			cb.Level(), // level
			1,          // commandBufferCount
		)).Ptr(),
		sb.MustAllocWriteData(cb.VulkanHandle()).Ptr(),
		VkResult_VK_SUCCESS,
	))

	if cb.Recording() == RecordingState_NOT_STARTED {
		return
	}

	beginInfo := NewVkCommandBufferBeginInfo(
		VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, // sType
		0, // pNext
		VkCommandBufferUsageFlags(cb.BeginInfo().Flags()), // flags
		0, // pInheritanceInfo
	)
	if cb.BeginInfo().Inherited() {
		inheritanceInfo := sb.MustAllocReadData(NewVkCommandBufferInheritanceInfo(
			VkStructureType_VK_STRUCTURE_TYPE_COMMAND_BUFFER_INHERITANCE_INFO, // sType
			0, // pNext
			cb.BeginInfo().InheritedRenderPass(),         // renderPass
			cb.BeginInfo().InheritedSubpass(),            // subpass
			cb.BeginInfo().InheritedFramebuffer(),        // framebuffer
			cb.BeginInfo().InheritedOcclusionQuery(),     // occlusionQueryEnable
			cb.BeginInfo().InheritedQueryFlags(),         // queryFlags
			cb.BeginInfo().InheritedPipelineStatsFlags(), // pipelineStatistics
		))
		beginInfo.SetPInheritanceInfo(NewVkCommandBufferInheritanceInfoᶜᵖ(inheritanceInfo.Ptr()))
	}

	sb.write(sb.cb.VkBeginCommandBuffer(
		cb.VulkanHandle(),
		sb.MustAllocReadData(beginInfo).Ptr(),
		VkResult_VK_SUCCESS,
	))

	hasError := false
	// fill command buffer
	for i, c := uint32(0), uint32(cb.CommandReferences().Len()); i < c; i++ {
		arg := GetCommandArgs(sb.ctx, cb.CommandReferences().Get(i), GetState(sb.oldState))
		cleanup, cmd, err := AddCommand(sb.ctx, sb.cb, cb.VulkanHandle(), sb.oldState, sb.newState, arg)
		if err != nil {
			log.W(sb.ctx, "Command Buffer %v is invalid, it will not be recorded: - %v", cb.VulkanHandle(), err)
			hasError = true
			break
		}
		sb.write(cmd)
		cleanup()
	}
	if hasError {
		return
	}
	if cb.Recording() == RecordingState_COMPLETED {
		sb.write(sb.cb.VkEndCommandBuffer(
			cb.VulkanHandle(),
			VkResult_VK_SUCCESS,
		))
	}
}
