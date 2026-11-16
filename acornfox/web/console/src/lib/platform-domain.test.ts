import { describe, expect, it } from 'vitest'
import { normDomain, recordName } from './platform-domain'

describe('平台域名 recordName、normDomain', () => {
  it('到买域名的地方加的那条记录的名字', () => {
    expect(recordName('apps.example.com')).toBe('*.apps')
    expect(recordName('example.com')).toBe('*')
    expect(recordName('a.b.example.com')).toBe('*.a.b')
    expect(recordName('apps.example.com.cn')).toBe('*.apps')
    expect(recordName('example.com.cn')).toBe('*')
    expect(recordName('localhost')).toBe('')
    expect(recordName('')).toBe('')
  })
  it('填的域名去掉空白、协议、路径、末尾的点，转小写', () => {
    expect(normDomain(' https://Apps.Example.com/x?y ')).toBe('apps.example.com')
    expect(normDomain('apps.example.com.')).toBe('apps.example.com')
  })
})
