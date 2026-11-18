import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import Divider from '@mui/material/Divider';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareDeviceDetail } from 'api/shareGroups';

// The overview folds per-hour buckets into hour-of-day totals in the browser so
// the chart follows the viewer's timezone instead of the server's.
function foldHourOfDay(hourly) {
  const buckets = Array.from({ length: 24 }, (_, hour) => ({ hour, bytes: 0 }));
  for (const row of hourly || []) {
    const date = new Date(row.window_start);
    if (Number.isNaN(date.getTime())) continue;
    buckets[date.getHours()].bytes += row.bytes || 0;
  }
  return buckets;
}

function statusColor(status) {
  switch (status) {
    case 'active':
      return 'success';
    case 'pending':
      return 'warning';
    case 'blocked':
      return 'error';
    default:
      return 'default';
  }
}

function hourLabel(hour) {
  return `${String(hour).padStart(2, '0')}:00`;
}

export default function ShareDeviceDetailDialog({ open, hwid, deviceId, onClose }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open || !hwid || !deviceId) {
      setDetail(null);
      setError('');
      return undefined;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    getShareDeviceDetail({ hwid, deviceId })
      .then((res) => {
        if (!cancelled) setDetail(res.data || null);
      })
      .catch((err) => {
        if (!cancelled) setError(err.response?.data?.msg || t('shareGroups.telemetry.loadFailed'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, hwid, deviceId, t]);

  const device = detail?.device || null;
  const windowDays = detail?.window_days || 7;

  const hourBuckets = useMemo(() => foldHourOfDay(detail?.hourly), [detail]);
  const maxHourBytes = useMemo(() => Math.max(...hourBuckets.map((bucket) => bucket.bytes), 1), [hourBuckets]);
  const peakHour = useMemo(
    () => hourBuckets.reduce((best, bucket) => (bucket.bytes > best.bytes ? bucket : best), hourBuckets[0]),
    [hourBuckets]
  );

  const statTiles = useMemo(() => {
    if (!device) return [];
    return [
      { key: 'lifetime', label: t('shareGroups.telemetryDetail.stats.lifetime'), value: formatBytes(device.reported_traffic_bytes || 0) },
      {
        key: 'window',
        label: t('shareGroups.telemetryDetail.stats.window', { days: windowDays }),
        value: formatBytes(device.window_traffic_bytes || 0)
      },
      {
        key: 'firstSeen',
        label: t('shareGroups.telemetryDetail.stats.firstSeen'),
        value: device.first_seen_at ? formatDateTime(device.first_seen_at, language) : '-'
      },
      {
        key: 'lastSeen',
        label: t('shareGroups.telemetryDetail.stats.lastSeen'),
        value: device.last_seen_at ? formatDateTime(device.last_seen_at, language) : '-'
      },
      {
        key: 'lastReport',
        label: t('shareGroups.telemetryDetail.stats.lastReport'),
        value: device.last_report_at ? formatDateTime(device.last_report_at, language) : '-'
      }
    ];
  }, [device, windowDays, t, language]);

  const titleName = device?.name || deviceId;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth fullScreen={fullScreen}>
      <DialogTitle>{t('shareGroups.telemetryDetail.title', { name: titleName })}</DialogTitle>
      <DialogContent dividers>
        {loading ? (
          <Stack alignItems="center" sx={{ py: 6 }}>
            <CircularProgress />
          </Stack>
        ) : error ? (
          <Alert severity="error">{error}</Alert>
        ) : device ? (
          <Stack spacing={2.5}>
            <Box>
              <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                <Chip
                  size="small"
                  color={statusColor(device.status)}
                  label={t(`shareGroups.deviceStatus.${device.status || 'active'}`)}
                  variant="outlined"
                />
                {device.platform && <Chip size="small" label={device.platform} variant="outlined" />}
                {device.app_version && <Chip size="small" label={device.app_version} variant="outlined" />}
                <Chip size="small" variant="outlined" label={t('shareGroups.telemetry.shareCount', { count: device.share_count || 0 })} />
              </Stack>
              <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.75, wordBreak: 'break-all' }}>
                {t('shareGroups.telemetryDetail.identity', { hwid: device.hwid, deviceId: device.device_id })}
              </Typography>
            </Box>

            <Grid container spacing={1.25}>
              {statTiles.map((tile) => (
                <Grid key={tile.key} size={{ xs: 6, sm: 4 }}>
                  <Box sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1.5, height: '100%' }}>
                    <Typography variant="caption" color="text.secondary" display="block">
                      {tile.label}
                    </Typography>
                    <Typography variant="body2" fontWeight={600} noWrap>
                      {tile.value}
                    </Typography>
                  </Box>
                </Grid>
              ))}
            </Grid>

            <Box>
              <Typography variant="subtitle2" fontWeight={700}>
                {t('shareGroups.telemetryDetail.subscriptions')}
              </Typography>
              <Stack spacing={1} sx={{ mt: 1 }}>
                {(device.shares || []).length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    {t('shareGroups.telemetryDetail.noSubscriptions')}
                  </Typography>
                ) : (
                  device.shares.map((link) => (
                    <Box key={link.share_id} sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1.5 }}>
                      <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                        <Typography variant="body2" fontWeight={600}>
                          {link.subscription_name || `#${link.subscription_id}`}
                        </Typography>
                        {link.group_name && <Chip size="small" label={link.group_name} variant="outlined" />}
                        <Chip
                          size="small"
                          variant="outlined"
                          color={link.status === 'enabled' ? 'success' : 'default'}
                          label={link.status === 'enabled' ? t('shareGroups.deviceStatus.active') : t('shareGroups.deviceStatus.disabled')}
                        />
                        <Box sx={{ flexGrow: 1 }} />
                        <Typography variant="caption" color="text.secondary">
                          {formatBytes(link.reported_traffic_bytes || 0)}
                        </Typography>
                      </Stack>
                      <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.25 }}>
                        {link.share_name || `#${link.share_id}`}
                      </Typography>
                      <Typography variant="caption" color="text.secondary" display="block">
                        {t('shareGroups.telemetry.columns.lastSeen')}:{' '}
                        {link.last_seen_at ? formatDateTime(link.last_seen_at, language) : '-'} ·{' '}
                        {t('shareGroups.telemetry.columns.lastReport')}:{' '}
                        {link.last_report_at ? formatDateTime(link.last_report_at, language) : '-'}
                      </Typography>
                    </Box>
                  ))
                )}
              </Stack>
            </Box>

            <Divider />

            <Box>
              <Stack direction="row" alignItems="baseline" spacing={1} flexWrap="wrap" useFlexGap>
                <Typography variant="subtitle2" fontWeight={700}>
                  {t('shareGroups.telemetryDetail.hourly')}
                </Typography>
                {peakHour && peakHour.bytes > 0 && (
                  <Typography variant="caption" color="text.secondary">
                    {t('shareGroups.telemetryDetail.peakHour', { hour: hourLabel(peakHour.hour) })}
                  </Typography>
                )}
              </Stack>
              <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
                {t('shareGroups.telemetryDetail.hourlyHint', { days: windowDays })}
              </Typography>
              {(detail?.hourly || []).length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t('shareGroups.telemetryDetail.hourlyEmpty')}
                </Typography>
              ) : (
                <>
                  <Stack direction="row" spacing={0.5} alignItems="flex-end" sx={{ height: 120 }}>
                    {hourBuckets.map((bucket) => {
                      const active = bucket.bytes > 0;
                      const isPeak = active && bucket.hour === peakHour.hour;
                      return (
                        <Tooltip key={bucket.hour} title={`${hourLabel(bucket.hour)} — ${formatBytes(bucket.bytes)}`} arrow>
                          <Box
                            sx={{
                              flex: 1,
                              height: '100%',
                              display: 'flex',
                              alignItems: 'flex-end',
                              cursor: 'default'
                            }}
                          >
                            <Box
                              sx={{
                                width: '100%',
                                minHeight: 2,
                                height: active ? `${Math.max((bucket.bytes / maxHourBytes) * 100, 4)}%` : '2px',
                                borderRadius: 0.5,
                                bgcolor: isPeak ? 'primary.main' : 'primary.light',
                                opacity: active ? 1 : 0.25
                              }}
                            />
                          </Box>
                        </Tooltip>
                      );
                    })}
                  </Stack>
                  <Stack direction="row" justifyContent="space-between" sx={{ mt: 0.5 }}>
                    {[0, 6, 12, 18, 23].map((hour) => (
                      <Typography key={hour} variant="caption" color="text.secondary">
                        {hourLabel(hour)}
                      </Typography>
                    ))}
                  </Stack>
                </>
              )}
            </Box>

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.telemetryDetail.activity')}
              </Typography>
              {(detail?.activity || []).length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t('shareGroups.telemetryDetail.activityEmpty')}
                </Typography>
              ) : (
                <Stack spacing={0.75}>
                  {detail.activity.map((entry) => (
                    <Stack
                      key={entry.id}
                      direction="row"
                      spacing={1}
                      alignItems="center"
                      sx={{ py: 0.5, borderBottom: '1px solid', borderColor: 'divider' }}
                    >
                      <Chip
                        size="small"
                        variant="outlined"
                        label={t(`shareGroups.activityDialog.actions.${entry.action}`, { defaultValue: entry.action })}
                      />
                      <Typography variant="body2" noWrap sx={{ flexGrow: 1, minWidth: 0 }}>
                        {entry.share_name || `#${entry.share_id}`}
                      </Typography>
                      {entry.reason && (
                        <Typography variant="caption" color="text.secondary" noWrap>
                          {t(`shareGroups.activityDialog.reasons.${entry.reason}`, { defaultValue: entry.reason })}
                        </Typography>
                      )}
                      <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
                        {formatDateTime(entry.created_at, language)}
                      </Typography>
                    </Stack>
                  ))}
                </Stack>
              )}
            </Box>
          </Stack>
        ) : null}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}
