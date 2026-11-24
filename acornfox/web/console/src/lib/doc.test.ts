import { describe, expect, it } from 'vitest'
import { bilibiliEmbed, docHasVideo, docSection, parseDoc, videoKind } from './doc'

describe('介绍文档', () => {
  const doc = [
    '## 它能帮你做什么', '', '- 记笔记', '- 找东西', '',
    '[演示](https://www.bilibili.com/video/BV1xx411c7mD)', '',
    '![封面](https://acornfox.com/catalog/assets/abc.png)', '',
    '## 装好以后', '', '1. 打开它', '2. 设个密码',
  ].join('\n')
  it('标题、列表、视频、图各成一块', () => {
    expect(parseDoc(doc).map((b) => b.t)).toEqual(['h', 'ul', 'video', 'img', 'h', 'ol'])
  })
  it('某个小标题下面的列表', () => {
    expect(docSection(doc, '装好以后')).toEqual(['打开它', '设个密码'])
    expect(docSection(doc, '没有这一节')).toEqual([])
    expect(docSection(undefined, '装好以后')).toEqual([])
  })
  it('视频只有“有”和“还没有”两种：带 BV 号、短链接、视频文件都算有；只写到 /video/ 的算还没有；样稿用的 /video/demo 不再特别对待', () => {
    expect(videoKind('https://www.bilibili.com/video/BV1xx411c7mD?p=2')).toBe('real')
    expect(videoKind('https://b23.tv/abc')).toBe('real')
    expect(videoKind('https://example.com/a.mp4')).toBe('real')
    expect(videoKind('https://www.bilibili.com/video/')).toBe('none')
    expect(docHasVideo(doc)).toBe(true)
    expect(docHasVideo('[还没有](https://www.bilibili.com/video/)')).toBe(false)
    expect(docHasVideo('只有字')).toBe(false)
    expect(docHasVideo(undefined)).toBe(false)
  })
  it('哔哩哔哩的播放器地址', () => {
    expect(bilibiliEmbed('https://www.bilibili.com/video/BV1xx411c7mD?p=2')).toContain('bvid=BV1xx411c7mD&p=2')
    expect(bilibiliEmbed('https://b23.tv/abc')).toBe('')
  })
})
