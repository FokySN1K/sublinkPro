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
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareSubscriptionDetail } from 'api/shareGroups';

import { ShareDailyBarChart, ShareReliabilityList, ShareSessionsTable, ShareTrafficBreakdown } from './ShareTelemetryBlocks';

// Detail fetches are capped so a user with many cards still opens quickly.
const DETAIL_SHARE_LIMIT = 8;

function sortByDateDesc(rows, field) {
  return [...rows].sort((left, right) => new Date(right[field] || 0) - new Date(left[field] || 0));
}

// User detail: merges the per-link detail responses of the client's most
// active cards into one aggregated view.
export default function ShareUserDetailDialog({ open, group, days, onClose }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [loading, setLoading] = useState(false);
  const [details, setDetails] = useState([]);
  const [error, setError] = useState('');

  const items = useMemo(() => group?.items || [], [group]);
  const targetShares = useMemo(
    () =>
      [...items]
        .sort((left, right) => {
          if (Boolean(left.online_now) !== Boolean(right.online_now)) return left.online_now ? -1 : 1;
          return (right.window_bytes || 0) - (left.window_bytes || 0);
        })
        .slice(0, DETAIL_SHARE_LIMIT),
    [items]
  );

  useEffect(() => {
    if (!open || targetShares.length === 0) {
      setDetails([]);
      setError('');
      return undefined;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    Promise.all(
      targetShares.map((item) =>
        getShareSubscriptionDetail({ shareId: item.share_id, days })
          .then((res) => res.data)
          .catch(() => null)
      )
    )
      .then((rows) => {
        if (cancelled) return;
        setDetails(rows.filter(Boolean));
      })
      .catch((err) => {
        if (!cancelled) setError(err.response?.data?.msg || t('shareGroups.userDetail.loadFailed'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, targetShares, days, t]);

  const merged = useMemo(() => {
    const dailyMap = new Map();
    const nodeMap = new Map();
    const airportMap = new Map();
    const reliabilityMap = new Map();
    const deviceMap = new Map();
    let sessions = [];
    let reliabilityEvents = [];
    let events = [];
    let lifetimeBytes = 0;

    for (const detail of details) {
      const share = detail.share || {};
      const shareLabel = share.share_name || `#${share.share_id}`;
      for (const bucket of detail.daily || []) {
        dailyMap.set(bucket.day, (dailyMap.get(bucket.day) || 0) + (bucket.bytes || 0));
      }
      for (const session of detail.sessions || []) {
        sessions.push({ ...session, share_id: share.share_id, share_name: shareLabel });
      }
      for (const node of detail.nodes || []) {
        const current = nodeMap.get(node.key) || { ...node, bytes: 0, up_bytes: 0, down_bytes: 0 };
        current.bytes += node.bytes || 0;
        current.up_bytes += node.up_bytes || 0;
        current.down_bytes += node.down_bytes || 0;
        nodeMap.set(node.key, current);
      }
      for (const airport of detail.airports || []) {
        const key = `${airport.airport_id}`;
        const current = airportMap.get(key) || { ...airport, bytes: 0, share: 0 };
        current.bytes += airport.bytes || 0;
        airportMap.set(key, current);
      }
      for (const row of detail.reliability || []) {
        const key = `${row.key}`;
        if (!reliabilityMap.has(key)) reliabilityMap.set(key, row);
      }
      reliabilityEvents = reliabilityEvents.concat(detail.reliability_events || []);
      events = events.concat((detail.events || []).map((entry) => ({ ...entry, share_name: shareLabel })));
      for (const device of detail.devices || []) {
        const key = `${device.hwid}-${device.device_id}`;
        const current = deviceMap.get(key);
        if (current) {
          current.window_traffic_bytes += device.window_traffic_bytes || 0;
          current.reported_traffic_bytes += device.reported_traffic_bytes || 0;
          current.sessions_count += device.sessions_count || 0;
          if (new Date(device.last_seen_at || 0) > new Date(current.last_seen_at || 0)) current.last_seen_at = device.last_seen_at;
        } else {
          deviceMap.set(key, { ...device, share_name: shareLabel });
        }
      }
      lifetimeBytes += share.reported_traffic_bytes || 0;
    }

    const daily = Array.from(dailyMap.entries())
      .sort(([left], [right]) => (left < right ? -1 : 1))
      .map(([day, bytes]) => ({ day, bytes }));
    const nodes = Array.from(nodeMap.values()).sort((left, right) => right.bytes - left.bytes);
    const airportTotal = Array.from(airportMap.values()).reduce((sum, row) => sum + row.bytes, 0);
    const airports = Array.from(airportMap.values())
      .map((row) => ({ ...row, share: airportTotal > 0 ? row.bytes / airportTotal : 0 }))
      .sort((left, right) => right.bytes - left.bytes);
    const devices = sortByDateDesc(Array.from(deviceMap.values()), 'last_seen_at').slice(0, 50);

    return {
      daily,
      nodes,
      airports,
      devices,
      sessions: sortByDateDesc(sessions, 'started_at').slice(0, 100),
      reliability: Array.from(reliabilityMap.values()).sort((left, right) => left.score - right.score),
      reliabilityEvents: sortByDateDesc(reliabilityEvents, 'created_at').slice(0, 100),
      events: sortByDateDesc(events, 'created_at').slice(0, 30),
      lifetimeBytes
    };
  }, [details]);

  const summary = group?.summary || null;
  const title = group?.client?.name || t('shareGroups.users.unassigned');

  const statTiles = useMemo(() => {
    if (!summary) return [];
    return [
      { key: 'shares', label: t('shareGroups.userDetail.stats.shares'), value: `${summary.shares}` },
      { key: 'devices', label: t('shareGroups.userDetail.stats.devices'), value: `${summary.devices}` },
      { key: 'online', label: t('shareGroups.userDetail.stats.online'), value: `${summary.online}` },
      { key: 'window', label: t('shareGroups.userDetail.stats.window', { days }), value: formatBytes(summary.windowBytes) },
      { key: 'lifetime', label: t('shareGroups.userDetail.stats.lifetime'), value: formatBytes(summary.lifetimeBytes) },
      { key: 'problems', label: t('shareGroups.userDetail.stats.problemNodes'), value: `${summary.problemNodes}` }
    ];
  }, [summary, days, t]);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth fullScreen={fullScreen}>
      <DialogTitle>{t('shareGroups.userDetail.title', { name: title })}</DialogTitle>
      <DialogContent dividers>
        {loading ? (
          <Stack alignItems="center" sx={{ py: 6 }}>
            <CircularProgress />
          </Stack>
        ) : error ? (
          <Alert severity="error">{error}</Alert>
        ) : (
          <Stack spacing={2.5}>
            {group?.client?.note && (
              <Typography variant="body2" color="text.secondary">
                {group.client.note}
              </Typography>
            )}
            {targetShares.length < items.length && (
              <Alert severity="info" variant="outlined">
                {t('shareGroups.userDetail.mergeNote', { count: DETAIL_SHARE_LIMIT })}
              </Alert>
            )}

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
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.shares')}
              </Typography>
              {items.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t('shareGroups.userDetail.sharesEmpty')}
                </Typography>
              ) : (
                <Stack direction="row" spacing={0.75} flexWrap="wrap" useFlexGap>
                  {items.map((item) => (
                    <Chip
                      key={item.share_id}
                      size="small"
                      variant="outlined"
                      color={item.online_now ? 'success' : 'default'}
                      label={item.share_name || `#${item.share_id}`}
                    />
                  ))}
                </Stack>
              )}
            </Box>

            <Divider />

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.daily')}
              </Typography>
              <ShareDailyBarChart daily={merged.daily} />
            </Box>

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.devices')}
              </Typography>
              {merged.devices.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t('shareGroups.subscriptionDetail.devicesEmpty')}
                </Typography>
              ) : (
                <Stack spacing={1}>
                  {merged.devices.map((device) => (
                    <Stack
                      key={`${device.hwid}-${device.device_id}`}
                      direction="row"
                      spacing={1}
                      alignItems="center"
                      sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1.5 }}
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
                          {[device.platform, device.share_name].filter(Boolean).join(' · ') || device.hwid}
                        </Typography>
                      </Box>
                      <Box sx={{ textAlign: 'right' }}>
                        <Typography variant="body2">{formatBytes(device.window_traffic_bytes || 0)}</Typography>
                        <Typography variant="caption" color="text.secondary" display="block">
                          {device.last_seen_at ? formatDateTime(device.last_seen_at, language) : '-'}
                        </Typography>
                      </Box>
                    </Stack>
                  ))}
                </Stack>
              )}
            </Box>

            <Divider />

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.sessions')}
              </Typography>
              <ShareSessionsTable sessions={merged.sessions} showShare showDevice />
            </Box>

            <Divider />

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.traffic')}
              </Typography>
              <ShareTrafficBreakdown nodes={merged.nodes} airports={merged.airports} />
            </Box>

            <Divider />

            <ShareReliabilityList reliability={merged.reliability} events={merged.reliabilityEvents} />

            <Divider />

            <Box>
              <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
                {t('shareGroups.userDetail.activity')}
              </Typography>
              {merged.events.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  {t('shareGroups.subscriptionDetail.activityEmpty')}
                </Typography>
              ) : (
                <Stack spacing={0.75}>
                  {merged.events.map((entry) => (
                    <Stack
                      key={`${entry.share_id}-${entry.id}`}
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
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}
