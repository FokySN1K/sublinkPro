import { useCallback, useEffect, useMemo, useState } from 'react';
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
import LinearProgress from '@mui/material/LinearProgress';
import MenuItem from '@mui/material/MenuItem';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareClients, getShareSubscriptionDetail, assignShareClients } from 'api/shareGroups';

import { ShareDailyBarChart, ShareReliabilityList, ShareSessionsTable, ShareTrafficBreakdown } from './ShareTelemetryBlocks';
import ShareDeviceDetailDialog from './ShareDeviceDetailDialog';

function statusColor(status) {
  switch (status) {
    case 'enabled':
      return 'success';
    case 'expired':
      return 'warning';
    default:
      return 'default';
  }
}

// Subscription detail: quota, daily traffic, devices, sessions, node traffic,
// reliability and recent access records for one share link.
export default function ShareSubscriptionDetailDialog({ open, shareId, days, onClose, onChanged, showMessage }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState('');
  const [clients, setClients] = useState([]);
  const [assigning, setAssigning] = useState(false);
  const [deviceTarget, setDeviceTarget] = useState(null);

  const loadDetail = useCallback(async () => {
    if (!shareId) return;
    setLoading(true);
    setError('');
    try {
      const res = await getShareSubscriptionDetail({ shareId, days });
      setDetail(res.data || null);
    } catch (err) {
      setDetail(null);
      setError(err.response?.data?.msg || t('shareGroups.subscriptionDetail.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [shareId, days, t]);

  useEffect(() => {
    if (!open || !shareId) {
      setDetail(null);
      setError('');
      return undefined;
    }
    loadDetail();
    getShareClients()
      .then((res) => setClients(res.data?.items || []))
      .catch(() => setClients([]));
    return undefined;
  }, [open, shareId, loadDetail]);

  const share = detail?.share || null;
  const windowDays = detail?.window_days || days || 7;

  const totals = useMemo(() => {
    const devices = detail?.devices || [];
    const windowBytes = devices.reduce((sum, device) => sum + (device.window_traffic_bytes || 0), 0);
    const sessions = detail?.sessions || [];
    return {
      windowBytes,
      sessions: sessions.length,
      openSessions: sessions.filter((session) => session.open).length
    };
  }, [detail]);

  const statTiles = useMemo(() => {
    if (!share) return [];
    const limit = share.traffic_limit_bytes || 0;
    return [
      { key: 'lifetime', label: t('shareGroups.subscriptionDetail.stats.lifetime'), value: formatBytes(share.reported_traffic_bytes || 0) },
      {
        key: 'window',
        label: t('shareGroups.subscriptionDetail.stats.window', { days: windowDays }),
        value: formatBytes(totals.windowBytes)
      },
      {
        key: 'remaining',
        label: t('shareGroups.subscriptionDetail.stats.remaining'),
        value: limit > 0 ? formatBytes(share.remaining_bytes || 0) : t('shareGroups.subscriptions.quota.unlimited')
      },
      { key: 'devices', label: t('shareGroups.subscriptionDetail.stats.devices'), value: `${(detail?.devices || []).length}` },
      {
        key: 'sessions',
        label: t('shareGroups.subscriptionDetail.stats.sessions'),
        value: `${totals.sessions}${totals.openSessions > 0 ? ` (${t('shareGroups.subscriptions.online')})` : ''}`
      },
      { key: 'access', label: t('shareGroups.subscriptionDetail.stats.accessCount'), value: `${share.access_count || 0}` },
      { key: 'created', label: t('shareGroups.subscriptionDetail.stats.createdAt'), value: formatDateTime(share.created_at, language) },
      {
        key: 'lastAccess',
        label: t('shareGroups.subscriptionDetail.stats.lastAccess'),
        value: share.last_access_at ? formatDateTime(share.last_access_at, language) : '-'
      }
    ];
  }, [share, detail, totals, windowDays, t, language]);

  const handleAssign = useCallback(
    async (event) => {
      const value = event.target.value;
      setAssigning(true);
      try {
        await assignShareClients({ share_ids: [shareId], client_id: value === '' ? null : Number(value) });
        showMessage?.(t('shareGroups.subscriptionDetail.assigned'));
        await loadDetail();
        onChanged?.();
      } catch (err) {
        showMessage?.(err.response?.data?.msg || t('shareGroups.messages.saveFailed'), 'error');
      } finally {
        setAssigning(false);
      }
    },
    [shareId, showMessage, t, loadDetail, onChanged]
  );

  const limit = share?.traffic_limit_bytes || 0;
  const used = share?.reported_traffic_bytes || 0;
  const percent = limit > 0 ? Math.min(Math.round((used / limit) * 1000) / 10, 100) : 0;

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth fullScreen={fullScreen}>
        <DialogTitle>{t('shareGroups.subscriptionDetail.title', { name: share?.share_name || `#${shareId}` })}</DialogTitle>
        <DialogContent dividers>
          {loading ? (
            <Stack alignItems="center" sx={{ py: 6 }}>
              <CircularProgress />
            </Stack>
          ) : error ? (
            <Alert severity="error">{error}</Alert>
          ) : share ? (
            <Stack spacing={2.5}>
              <Box>
                <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                  <Chip
                    size="small"
                    color={statusColor(share.status)}
                    variant="outlined"
                    label={t(`shareGroups.subscriptions.status.${share.status || 'enabled'}`)}
                  />
                  {share.quota_exhausted && (
                    <Chip size="small" color="error" variant="outlined" label={t('shareGroups.subscriptions.quota.exhausted')} />
                  )}
                  {share.group_name && <Chip size="small" label={share.group_name} variant="outlined" />}
                  <Chip
                    size="small"
                    variant="outlined"
                    color={share.telemetry_enabled ? 'success' : 'default'}
                    label={
                      share.telemetry_enabled ? t('shareGroups.fields.telemetryEnabled') : t('shareGroups.subscriptionDetail.telemetryOff')
                    }
                  />
                </Stack>
                <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.75 }}>
                  {[share.subscription_name || `#${share.subscription_id}`, share.group_name].filter(Boolean).join(' · ')}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  display="block"
                  sx={{ wordBreak: 'break-all', fontFamily: 'monospace' }}
                >
                  {share.token}
                </Typography>
              </Box>

              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} alignItems={{ sm: 'center' }}>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="caption" color="text.secondary" display="block">
                    {t('shareGroups.subscriptions.columns.quota')}
                  </Typography>
                  {limit > 0 ? (
                    <>
                      <LinearProgress
                        variant="determinate"
                        value={percent}
                        color={share.quota_exhausted ? 'error' : percent >= 80 ? 'warning' : 'primary'}
                        sx={{ height: 6, borderRadius: 3, my: 0.5 }}
                      />
                      <Typography variant="caption" color="text.secondary">
                        {formatBytes(used)} / {formatBytes(limit)}
                      </Typography>
                    </>
                  ) : (
                    <Typography variant="body2" color="text.secondary">
                      {formatBytes(used)} · {t('shareGroups.subscriptions.quota.unlimited')}
                    </Typography>
                  )}
                </Box>
                <TextField
                  select
                  size="small"
                  label={t('shareGroups.subscriptionDetail.client')}
                  value={share.client_id ? `${share.client_id}` : ''}
                  onChange={handleAssign}
                  disabled={assigning}
                  sx={{ minWidth: 200 }}
                >
                  <MenuItem value="">{t('shareGroups.subscriptionDetail.noClient')}</MenuItem>
                  {clients.map((client) => (
                    <MenuItem key={client.id} value={`${client.id}`}>
                      {client.name}
                    </MenuItem>
                  ))}
                </TextField>
              </Stack>

              <Grid container spacing={1.25}>
                {statTiles.map((tile) => (
                  <Grid key={tile.key} size={{ xs: 6, sm: 3 }}>
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
                <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                  {t('shareGroups.subscriptionDetail.daily')}
                </Typography>
                <ShareDailyBarChart daily={detail.daily || []} />
              </Box>

              <Divider />

              <Box>
                <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                  {t('shareGroups.subscriptionDetail.devices')}
                </Typography>
                {(detail.devices || []).length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    {t('shareGroups.subscriptionDetail.devicesEmpty')}
                  </Typography>
                ) : (
                  <Stack spacing={1}>
                    {detail.devices.map((device) => (
                      <Stack
                        key={device.id}
                        direction="row"
                        spacing={1}
                        alignItems="center"
                        onClick={() => setDeviceTarget({ hwid: device.hwid, deviceId: device.device_id })}
                        sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1.5, cursor: 'pointer' }}
                      >
                        <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                          <Stack direction="row" spacing={0.75} alignItems="center" flexWrap="wrap" useFlexGap>
                            <Typography variant="body2" fontWeight={600} noWrap>
                              {device.name || device.device_id}
                            </Typography>
                            <Chip
                              size="small"
                              variant="outlined"
                              color={device.status === 'active' ? 'success' : device.status === 'pending' ? 'warning' : 'error'}
                              label={t(`shareGroups.deviceStatus.${device.status || 'active'}`)}
                            />
                          </Stack>
                          <Typography variant="caption" color="text.secondary" display="block" noWrap>
                            {[device.platform, device.app_version].filter(Boolean).join(' · ') || device.hwid}
                          </Typography>
                        </Box>
                        <Box sx={{ textAlign: 'right' }}>
                          <Typography variant="body2">{formatBytes(device.window_traffic_bytes || 0)}</Typography>
                          <Typography variant="caption" color="text.secondary" display="block">
                            {t('shareGroups.subscriptionDetail.deviceSessions', { count: device.sessions_count || 0 })}
                          </Typography>
                        </Box>
                        <ChevronRightIcon fontSize="small" color="action" />
                      </Stack>
                    ))}
                  </Stack>
                )}
              </Box>

              <Divider />

              <Box>
                <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                  {t('shareGroups.subscriptionDetail.sessions')}
                </Typography>
                <ShareSessionsTable sessions={detail.sessions || []} showDevice />
              </Box>

              <Divider />

              <Box>
                <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                  {t('shareGroups.subscriptionDetail.traffic')}
                </Typography>
                <ShareTrafficBreakdown nodes={detail.nodes || []} airports={detail.airports || []} />
              </Box>

              <Divider />

              <ShareReliabilityList reliability={detail.reliability || []} events={detail.reliability_events || []} />

              <Divider />

              <Box>
                <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                  {t('shareGroups.subscriptionDetail.activity')}
                </Typography>
                {(detail.events || []).length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    {t('shareGroups.subscriptionDetail.activityEmpty')}
                  </Typography>
                ) : (
                  <Stack spacing={0.75}>
                    {detail.events.map((entry) => (
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

      <ShareDeviceDetailDialog
        open={Boolean(deviceTarget)}
        hwid={deviceTarget?.hwid}
        deviceId={deviceTarget?.deviceId}
        days={days}
        onClose={() => setDeviceTarget(null)}
      />
    </>
  );
}
