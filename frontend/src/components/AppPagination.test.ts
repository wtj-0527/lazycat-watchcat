import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AppPagination from './AppPagination.vue'

describe('AppPagination', () => {
  it('changes pages and exposes accessible controls', async () => {
    const updatePage = vi.fn()
    const wrapper = mount(AppPagination, {
      props: { total: 45, pageCount: 5, rangeStart: 1, rangeEnd: 10, page: 1, pageSize: 10, 'onUpdate:page': updatePage },
    })
    expect(wrapper.text()).toContain('第 1–10 条，共 45 条')
    expect(wrapper.get('[aria-label="列表分页上一页"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="列表分页下一页"]').trigger('click')
    expect(updatePage).toHaveBeenLastCalledWith(2)
    await wrapper.get('[aria-label="列表分页第 4 页"]').trigger('click')
    expect(updatePage).toHaveBeenLastCalledWith(4)
  })

  it('changes the page size and stays hidden for an empty list', async () => {
    const updatePageSize = vi.fn()
    const wrapper = mount(AppPagination, {
      props: { total: 28, pageCount: 3, rangeStart: 1, rangeEnd: 10, page: 1, pageSize: 10, 'onUpdate:pageSize': updatePageSize },
    })
    await wrapper.get('select').setValue('20')
    expect(updatePageSize).toHaveBeenLastCalledWith(20)
    await wrapper.setProps({ total: 0, rangeStart: 0, rangeEnd: 0 })
    expect(wrapper.find('nav').exists()).toBe(false)
  })

  it('shows a custom current page size even when it is not in the default choices', () => {
    const wrapper = mount(AppPagination, {
      props: { total: 27, pageCount: 3, rangeStart: 1, rangeEnd: 12, page: 1, pageSize: 12 },
    })

    expect(wrapper.get('select').element.value).toBe('12')
    expect(wrapper.get('select').text()).toContain('12 条')
  })
})
