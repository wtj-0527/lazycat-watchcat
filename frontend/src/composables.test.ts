import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePagination, usePolling } from './composables'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('usePolling', () => {
  it('does not expose a superseded response as data or readback evidence', async () => {
    const mounted = deferred<string>()
    const older = deferred<string>()
    const newest = deferred<string>()
    const requests = [mounted, older, newest]
    let request = 0
    const wrapper = mount(defineComponent({
      setup() {
        return usePolling(() => requests[request++].promise, 60_000)
      },
      template: '<span>{{ data }}</span>',
    }))

    const olderRefresh = wrapper.vm.refresh()
    const newestRefresh = wrapper.vm.refresh()
    newest.resolve('new')
    expect(await newestRefresh).toBe('new')
    await flushPromises()
    expect(wrapper.text()).toBe('new')

    older.resolve('old')
    expect(await olderRefresh).toBeUndefined()
    await flushPromises()
    expect(wrapper.text()).toBe('new')

    mounted.resolve('mounted-old')
    await flushPromises()
    expect(wrapper.text()).toBe('new')

    wrapper.unmount()
  })

  it('waits for a polling response before scheduling the next interval', async () => {
    vi.useFakeTimers()
    const slow = deferred<string>()
    const loader = vi.fn()
      .mockImplementationOnce(() => slow.promise)
      .mockResolvedValue('next')
    const wrapper = mount(defineComponent({
      setup() {
        return usePolling(loader, 30_000)
      },
      template: '<span>{{ data }}</span>',
    }))

    expect(loader).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(90_000)
    expect(loader).toHaveBeenCalledTimes(1)

    slow.resolve('first')
    await flushPromises()
    expect(wrapper.text()).toBe('first')

    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()
    expect(loader).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toBe('next')

    wrapper.unmount()
    vi.useRealTimers()
  })
})

describe('usePagination', () => {
  it('slices the current page and corrects an out-of-range page when data shrinks', async () => {
    const items = ref(Array.from({ length: 25 }, (_, index) => index + 1))
    const pagination = usePagination(items, 10)
    pagination.page.value = 3
    expect(pagination.pagedItems.value).toEqual([21, 22, 23, 24, 25])
    expect(pagination.rangeStart.value).toBe(21)
    expect(pagination.rangeEnd.value).toBe(25)

    items.value = items.value.slice(0, 8)
    await nextTick()
    expect(pagination.page.value).toBe(1)
    expect(pagination.pagedItems.value).toEqual([1, 2, 3, 4, 5, 6, 7, 8])
  })
})
