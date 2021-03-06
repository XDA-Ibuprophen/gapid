/*
 * Copyright (C) 2017 Google Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// This file is intended to be included by vulkan_spy.h inside
// of the gapid namespace.

inline void VulkanSpy::notifyPendingCommandAdded(CallObserver*, VkQueue) {}

inline void VulkanSpy::vkErrInvalidHandle(CallObserver*, std::string handleType, uint64_t handle){
    GAPID_WARNING("Error: Invalid %s: %llu", handleType.c_str(), handle)
}

inline void VulkanSpy::vkErrNullPointer(CallObserver*, std::string pointerType) {
    GAPID_WARNING("Error: Null pointer of %s", pointerType.c_str())
}

inline void VulkanSpy::vkErrUnrecognizedExtension(CallObserver*, std::string name) {
    GAPID_WARNING("Error: Unrecognized extension: %s", name.c_str())
}

inline void VulkanSpy::vkErrExpectNVDedicatedlyAllocatedHandle(CallObserver*, std::string handleType, uint64_t handle) {
    GAPID_WARNING("Error: Expected handle that was allocated with a dedicated allocation: %s: %llu", handleType.c_str(), handle)
}

inline void VulkanSpy::vkErrInvalidDescriptorArrayElement(CallObserver*, uint64_t set, uint32_t binding, uint32_t array_index) {
  GAPID_WARNING("Error: Invalid descriptor array element specified by descriptor set: %llu, binding: %lu, array index: %lu", set, binding, array_index);
}

inline void VulkanSpy::vkErrCommandBufferIncomplete(CallObserver*, VkCommandBuffer cmdbuf) {
    GAPID_WARNING("Error: Executing command buffer %llu was not in the COMPLETED state", cmdbuf)
}

inline void VulkanSpy::vkErrImageLayout(CallObserver*, uint32_t layout, uint32_t expectedLayout) {
  GAPID_WARNING("Error: Image was in layout %lu, but was expected to be in layout %lu", layout, expectedLayout);
}
