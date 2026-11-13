// Cron 表达式验证
export const validateCronExpression = (cron) => {
  if (!cron) return false;
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return false;

  const ranges = [
    { min: 0, max: 59 }, // 分钟
    { min: 0, max: 23 }, // 小时
    { min: 1, max: 31 }, // 日
    { min: 1, max: 12 }, // 月
    { min: 0, max: 7 } // 星期 (0和7都表示周日)
  ];

  for (let i = 0; i < 5; i++) {
    const part = parts[i];
    const range = ranges[i];

    const segments = part.split(',');
    for (const segment of segments) {
      if (segment === '*') {
        continue;
      }

      const wildcardStepMatch = segment.match(/^\*\/(\d+)$/);
      if (wildcardStepMatch) {
        if (parseInt(wildcardStepMatch[1], 10) <= 0) {
          return false;
        }
        continue;
      }

      const stepMatch = segment.match(/^(\d+|\d+-\d+)\/(\d+)$/);
      if (stepMatch) {
        const step = parseInt(stepMatch[2], 10);
        if (step <= 0) {
          return false;
        }

        const [startRaw, endRaw] = stepMatch[1].split('-');
        const start = parseInt(startRaw, 10);
        const end = endRaw ? parseInt(endRaw, 10) : start;
        if (start > end) {
          return false;
        }
        if (start < range.min || end > range.max) {
          return false;
        }
        continue;
      }

      const rangeMatch = segment.match(/^(\d+)-(\d+)$/);
      if (rangeMatch) {
        const start = parseInt(rangeMatch[1], 10);
        const end = parseInt(rangeMatch[2], 10);
        if (start > end || start < range.min || end > range.max) {
          return false;
        }
        continue;
      }

      if (/^\d+$/.test(segment)) {
        const value = parseInt(segment, 10);
        if (value < range.min || value > range.max) {
          return false;
        }
        continue;
      }

      return false;
    }
  }
  return true;
};

// 格式化日期时间
export const formatDateTime = (dateTimeString) => {
  if (!dateTimeString || dateTimeString === '0001-01-01T00:00:00Z') {
    return '-';
  }
  try {
    const date = new Date(dateTimeString);
    if (isNaN(date.getTime())) {
      return '-';
    }
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const seconds = String(date.getSeconds()).padStart(2, '0');
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
  } catch {
    return '-';
  }
};

// User-Agent 预设选项
export const USER_AGENT_OPTIONS = [
  { labelKey: 'airports.form.userAgentOptions.none', value: '' },
  { label: 'clash.meta', value: 'clash.meta' },
  { label: 'clash', value: 'clash' },
  { label: 'v2ray', value: 'v2ray' },
  { label: 'clash-verge/v1.5.1', value: 'clash-verge/v1.5.1' }
];

// 设备伪装档位，与后端 models.AirportDeviceProfile* 常量保持一致
export const AIRPORT_DEVICE_PROFILE_OPTIONS = [
  { labelKey: 'airports.form.deviceProfile.options.none', value: '' },
  { labelKey: 'airports.form.deviceProfile.options.android', value: 'android' },
  { labelKey: 'airports.form.deviceProfile.options.ios', value: 'ios' },
  { labelKey: 'airports.form.deviceProfile.options.windows', value: 'windows' },
  { labelKey: 'airports.form.deviceProfile.options.macos', value: 'macos' },
  { labelKey: 'airports.form.deviceProfile.options.linux', value: 'linux' },
  { labelKey: 'airports.form.deviceProfile.options.custom', value: 'custom' }
];

// 设备档位预置值，与后端 models/airport_device.go 中的 airportDevicePresets 保持一致
export const AIRPORT_DEVICE_PROFILE_PRESETS = {
  android: { deviceOS: 'Android', osVersion: '14', deviceModel: 'Pixel 7', appName: 'v2rayTun' },
  ios: { deviceOS: 'iOS', osVersion: '17.4', deviceModel: 'iPhone15,2', appName: 'v2rayTun' },
  windows: { deviceOS: 'Windows', osVersion: '11', deviceModel: 'PC', appName: 'v2rayN' },
  macos: { deviceOS: 'macOS', osVersion: '14.5', deviceModel: 'MacBookPro18,3', appName: 'v2rayTun' },
  linux: { deviceOS: 'Linux', osVersion: '6.5', deviceModel: 'PC', appName: 'v2rayN' }
};

// 生成 32 位十六进制 HWID，用于新建机场时预填 X-HWID
export const generateHWIDValue = () => {
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  }
  const fallback = `${Date.now().toString(16)}${Math.random().toString(16).slice(2)}`;
  return fallback.padEnd(32, '0').slice(0, 32);
};

// 格式化字节为人类可读格式
export const formatBytes = (bytes, decimals = 2) => {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[Math.min(i, sizes.length - 1)];
};

// 格式化过期时间 - 直接显示日期
export const formatExpireTime = (timestamp) => {
  if (!timestamp || timestamp === 0) return '';

  const expireDate = new Date(timestamp * 1000);
  const year = expireDate.getFullYear();
  const month = String(expireDate.getMonth() + 1).padStart(2, '0');
  const day = String(expireDate.getDate()).padStart(2, '0');
  const hours = String(expireDate.getHours()).padStart(2, '0');
  const minutes = String(expireDate.getMinutes()).padStart(2, '0');
  const seconds = String(expireDate.getSeconds()).padStart(2, '0');
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
};

// 格式化过期日期（DD.MM.YYYY）
export const formatExpireDate = (timestamp) => {
  if (!timestamp || timestamp === 0) return '';

  const expireDate = new Date(timestamp * 1000);
  const day = String(expireDate.getDate()).padStart(2, '0');
  const month = String(expireDate.getMonth() + 1).padStart(2, '0');
  const year = expireDate.getFullYear();
  return `${day}.${month}.${year}`;
};

// 解析机场用量状态：disabled / failed / pending / ok
export const resolveAirportUsage = (airport) => {
  if (!airport || !airport.fetchUsageInfo) return { status: 'disabled' };
  if (airport.usageTotal === -1) return { status: 'failed' };

  const upload = airport.usageUpload || 0;
  const download = airport.usageDownload || 0;
  const used = upload + download;
  const total = airport.usageTotal || 0;
  const unlimited = total <= 0;
  const expire = airport.usageExpire || 0;

  // 未获取到任何用量且没有到期时间，视为待获取
  if (unlimited && used === 0 && expire === 0) return { status: 'pending' };

  let daysLeft = null;
  let expired = false;
  if (expire > 0) {
    const now = Math.floor(Date.now() / 1000);
    daysLeft = (expire - now) / 86400;
    expired = daysLeft <= 0;
  }

  return {
    status: 'ok',
    upload,
    download,
    used,
    total,
    unlimited,
    expire,
    daysLeft,
    expired,
    percent: unlimited ? 0 : Math.min((used / total) * 100, 100)
  };
};

// 根据使用率百分比返回颜色
export const getUsageColor = (percent) => {
  if (percent < 60) return '#22c55e';
  if (percent < 85) return '#f59e0b';
  return '#ef4444';
};
