import request from './request';

/**
 * 获取订阅组列表
 */
export function getShareGroups() {
  return request({
    url: '/v1/share-groups/list',
    method: 'get'
  });
}

/**
 * 获取订阅组详情（含Заглушка映射和订阅链接）
 * @param {number} id 订阅组ID
 */
export function getShareGroup(id) {
  return request({
    url: '/v1/share-groups/get',
    method: 'get',
    params: { id }
  });
}

/**
 * 创建订阅组
 */
export function createShareGroup(data) {
  return request({
    url: '/v1/share-groups/add',
    method: 'post',
    data
  });
}

/**
 * 更新订阅组
 */
export function updateShareGroup(data) {
  return request({
    url: '/v1/share-groups/update',
    method: 'post',
    data
  });
}

/**
 * 删除订阅组
 * @param {number} id 订阅组ID
 */
export function deleteShareGroup(id) {
  return request({
    url: '/v1/share-groups/delete',
    method: 'delete',
    params: { id }
  });
}

/**
 * 获取订阅组的Заглушка映射
 * @param {number} groupId 订阅组ID
 */
export function getShareGroupStubs(groupId) {
  return request({
    url: '/v1/share-groups/stubs',
    method: 'get',
    params: { groupId }
  });
}

/**
 * 更新订阅组的Заглушка映射
 * @param {number} groupId 订阅组ID
 * @param {object} stubs { case_key: stub_id }
 */
export function updateShareGroupStubs(groupId, stubs) {
  return request({
    url: '/v1/share-groups/stubs',
    method: 'post',
    data: { group_id: groupId, stubs }
  });
}

/**
 * 获取订阅组的访问记录
 * @param {number} groupId 订阅组ID
 */
export function getShareGroupActivity(groupId, limit = 200) {
  return request({
    url: '/v1/share-groups/activity',
    method: 'get',
    params: { groupId, limit }
  });
}

/**
 * 获取Заглушка库
 */
export function getShareStubs() {
  return request({
    url: '/v1/share-stubs/list',
    method: 'get'
  });
}

/**
 * 创建Заглушка
 */
export function createShareStub(data) {
  return request({
    url: '/v1/share-stubs/add',
    method: 'post',
    data
  });
}

/**
 * 更新Заглушка
 */
export function updateShareStub(data) {
  return request({
    url: '/v1/share-stubs/update',
    method: 'post',
    data
  });
}

/**
 * 删除Заглушка
 * @param {number} id ЗаглушкаID
 */
export function deleteShareStub(id) {
  return request({
    url: '/v1/share-stubs/delete',
    method: 'delete',
    params: { id }
  });
}

/**
 * 获取订阅链接的Заглушка覆盖
 * @param {number} shareId 分享ID
 */
export function getShareStubOverrides(shareId) {
  return request({
    url: '/v1/share-stubs/overrides',
    method: 'get',
    params: { shareId }
  });
}

/**
 * 更新订阅链接的Заглушка覆盖
 * @param {number} shareId 分享ID
 * @param {object} stubs { case_key: stub_id }
 */
export function updateShareStubOverrides(shareId, stubs) {
  return request({
    url: '/v1/share-stubs/overrides',
    method: 'post',
    data: { share_id: shareId, stubs }
  });
}

/**
 * 获取订阅链接绑定的设备
 * @param {number} shareId 分享ID
 */
export function getShareDevices(shareId) {
  return request({
    url: '/v1/shares/devices',
    method: 'get',
    params: { shareId }
  });
}

/**
 * 设备遥测总览：按安装（HWID + 设备ID）聚合，可按节点池 / 分组 / 状态过滤
 * @param {object} params { subId, groupId, status, keyword, page, pageSize }
 */
export function getShareDevicesOverview(params = {}) {
  return request({
    url: '/v1/shares/devices/overview',
    method: 'get',
    params
  });
}

/**
 * 单个设备的用量时间分布与访问记录
 * @param {object} params { hwid, deviceId }
 */
export function getShareDeviceDetail(params) {
  return request({
    url: '/v1/shares/devices/detail',
    method: 'get',
    params
  });
}

/**
 * 更新设备信息
 */
export function updateShareDevice(data) {
  return request({
    url: '/v1/shares/devices/update',
    method: 'post',
    data
  });
}

/**
 * 解绑设备
 * @param {number} id 设备ID
 */
export function deleteShareDevice(id) {
  return request({
    url: '/v1/shares/devices/delete',
    method: 'delete',
    params: { id }
  });
}

/**
 * 订阅链接遥测总览：每条分享链接一行（额度、设备、窗口流量、会话）
 * @param {object} params { subId, groupId, clientId, status, keyword, days, page, pageSize }
 */
export function getShareSubscriptionsOverview(params = {}) {
  return request({
    url: '/v1/shares/subscriptions/overview',
    method: 'get',
    params
  });
}

/**
 * 单条订阅链接详情：设备、按日流量、会话、节点/机场流量与可靠性
 * @param {object} params { shareId, days }
 */
export function getShareSubscriptionDetail(params) {
  return request({
    url: '/v1/shares/subscriptions/detail',
    method: 'get',
    params
  });
}

/**
 * 用户（客户端）列表
 * @param {object} params { keyword }
 */
export function getShareClients(params = {}) {
  return request({
    url: '/v1/shares/clients',
    method: 'get',
    params
  });
}

/**
 * 新建用户
 */
export function createShareClient(data) {
  return request({
    url: '/v1/shares/clients/add',
    method: 'post',
    data
  });
}

/**
 * 更新用户
 */
export function updateShareClient(data) {
  return request({
    url: '/v1/shares/clients/update',
    method: 'post',
    data
  });
}

/**
 * 删除用户（卡片保留，归属置空）
 * @param {number} id 用户ID
 */
export function deleteShareClient(id) {
  return request({
    url: '/v1/shares/clients/delete',
    method: 'delete',
    params: { id }
  });
}

/**
 * 将订阅卡片归属到用户（client_id 为空则取消归属）
 * @param {object} data { share_ids: number[], client_id: number|null }
 */
export function assignShareClients(data) {
  return request({
    url: '/v1/shares/clients/assign',
    method: 'post',
    data
  });
}

/**
 * 获取订阅链接的访问记录
 * @param {number} shareId 分享ID
 */
export function getShareActivity(shareId, limit = 200) {
  return request({
    url: '/v1/shares/activity',
    method: 'get',
    params: { shareId, limit }
  });
}
