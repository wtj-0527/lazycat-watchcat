import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usePolling } from './composables'

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
