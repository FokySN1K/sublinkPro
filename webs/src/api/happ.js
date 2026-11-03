import request from './request';

/**
 * happ:// 链接解密 API
 */

// 解密 happ://crypt / crypt2 / crypt3 / crypt4 / crypt5 链接
export const decryptHappLink = (link) => {
  return request({
    url: '/v1/tools/happ/decrypt',
    method: 'post',
    data: { link }
  });
};
