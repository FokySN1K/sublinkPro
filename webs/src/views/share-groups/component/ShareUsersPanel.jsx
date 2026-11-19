import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';
import Grid from '@mui/material/Grid';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';
import DevicesIcon from '@mui/icons-material/Devices';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareDevicesOverview } from 'api/shareGroups';

import ShareDeviceDetailDialog from './ShareDeviceDetailDialog';
import { formatDuration } from './ShareTelemetryBlocks';

const USER_DEVICE_LIMIT = 200;

function newerDevice(candidate, current) {
  if (!current) return true;
  const left = Date.parse(candidate.last_seen_at || 0) || 0;
  const right = Date.parse(current.last_seen_at || 0) || 0;
  return left > right;
}

// Users tab: one card per installation identity (HWID). One person may run the
// client on several devices, so a card folds every device row that shares the
// hardware id and answers where that installation sends its traffic.
export default function ShareUsersPanel({ days, showMessage, refreshKey }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;

  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [truncated, setTruncated] = useState(false);
  const [detailTarget, setDetailTarget] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getShareDevicesOverview({ days, page: 1, pageSize: USER_DEVICE_LIMIT });
      const data = res.data || {};
      setItems(data.items || []);
      setTruncated((data.total || 0) > USER_DEVICE_LIMIT);
    } catch (error) {
      setItems([]);
      showMessage?.(error.response?.data?.msg || t('shareGroups.users.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [days, showMessage, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData, refreshKey]);

  const groups = useMemo(() => {
    const byHwid = new Map();
    for (const item of items) {
      const key = item.hwid || item.device_id || item.name;
      if (!byHwid.has(key)) byHwid.set(key, []);
      byHwid.get(key).push(item);
    }

    const rows = [];
    for (const [key, list] of byHwid) {
      const newest = list.reduce((best, item) => (newerDevice(item, best) ? item : best), null);
      const shares = new Map();
      const airports = new Map();
      let sessionsToday = 0;
      let sessionsWindow = 0;
      let windowBytes = 0;
      let lifetimeBytes = 0;
      let problemNodes = 0;
      let lastSession = null;
      for (const item of list) {
        for (const link of item.shares || []) {
          shares.set(link.share_id, link);
        }
        for (const entry of item.airports || []) {
          const aggregate = airports.get(entry.airport_id) || {
            airport_id: entry.airport_id,
            airport_name: entry.airport_name,
            bytes: 0
          };
          aggregate.bytes += entry.bytes || 0;
          airports.set(entry.airport_id, aggregate);
        }
        sessionsToday += item.sessions_today || 0;
        sessionsWindow += item.sessions_window || 0;
        windowBytes += item.window_traffic_bytes || 0;
        lifetimeBytes += item.reported_traffic_bytes || 0;
        problemNodes += item.problem_nodes || 0;
        if (item.last_session) {
          const started = Date.parse(item.last_session.started_at || 0) || 0;
          if (!lastSession || started > (Date.parse(lastSession.started_at || 0) || 0)) {
            lastSession = item.last_session;
          }
        }
      }
      const topAirport = Array.from(airports.values()).sort((left, right) => right.bytes - left.bytes)[0] || null;
      rows.push({
        key,
        hwid: newest?.hwid || key,
        name: newest?.name || '',
        platform: newest?.platform || '',
        appVersion: newest?.app_version || '',
        deviceCount: new Set(list.map((item) => item.device_id)).size,
        sessionsToday,
        sessionsWindow,
        windowBytes,
        lifetimeBytes,
        problemNodes,
        lastSession,
        shares: Array.from(shares.values()),
        topAirport,
        lastSeenAt: newest?.last_seen_at,
        target: { hwid: newest?.hwid || key, deviceId: newest?.device_id }
      });
    }
    rows.sort((left, right) => {
      if (left.windowBytes !== right.windowBytes) return right.windowBytes - left.windowBytes;
      return (left.name || left.hwid).localeCompare(right.name || right.hwid);
    });
    return rows;
  }, [items]);

  const renderCard = (row) => (
    <Grid key={row.key} size={{ xs: 12, md: 6, xl: 4 }}>
      <Card
        sx={{ height: '100%', display: 'flex', flexDirection: 'column', border: '1px solid', borderColor: 'divider', cursor: 'pointer' }}
        onClick={() => setDetailTarget(row.target)}
      >
        <CardContent sx={{ flexGrow: 1 }}>
          <Stack direction="row" alignItems="flex-start" spacing={1}>
            <Box sx={{ minWidth: 0, flexGrow: 1 }}>
              <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                <Typography variant="subtitle1" fontWeight={700} noWrap>
                  {row.name || t('shareGroups.telemetry.unknownDevice')}
                </Typography>
                <Chip size="small" variant="outlined" label={t('shareGroups.users.devices', { count: row.deviceCount })} />
                <Chip size="small" variant="outlined" label={t('shareGroups.users.shareCount', { count: row.shares.length })} />
              </Stack>
              <Typography variant="caption" color="text.secondary" noWrap display="block">
                {[row.platform, row.appVersion].filter(Boolean).join(' · ') || '—'}
              </Typography>
              <Typography variant="caption" color="text.secondary" noWrap display="block" sx={{ fontFamily: 'monospace' }}>
                {row.hwid}
              </Typography>
            </Box>
          </Stack>

          <Divider sx={{ my: 1.5 }} />

          <Grid container spacing={1}>
            <Grid size={6}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.windowLabel', { days })}
              </Typography>
              <Typography variant="body2">{formatBytes(row.windowBytes)}</Typography>
            </Grid>
            <Grid size={6}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.sessionsLabel')}
              </Typography>
              <Typography variant="body2">{t('shareGroups.users.sessionsToday', { count: row.sessionsToday })}</Typography>
            </Grid>
            <Grid size={6}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.airportLabel')}
              </Typography>
              {row.topAirport ? (
                <Typography variant="body2" noWrap>
                  {row.topAirport.airport_name || t('shareGroups.blocks.airportUnknown')}
                </Typography>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  —
                </Typography>
              )}
            </Grid>
            <Grid size={6}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.lifetimeLabel')}
              </Typography>
              <Typography variant="body2">{formatBytes(row.lifetimeBytes)}</Typography>
            </Grid>
            <Grid size={12}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.lastVpn')}
              </Typography>
              {row.lastSession ? (
                <>
                  <Typography variant="body2" noWrap>
                    {formatDateTime(row.lastSession.started_at, language)} →{' '}
                    {row.lastSession.open ? t('shareGroups.blocks.sessionOpen') : formatDateTime(row.lastSession.ended_at, language)}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" display="block">
                    {formatDuration(row.lastSession.duration_s)} · {formatBytes(row.lastSession.bytes || 0)}
                  </Typography>
                </>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  —
                </Typography>
              )}
            </Grid>
            <Grid size={12}>
              <Typography variant="caption" color="text.secondary">
                {t('shareGroups.users.problemsLabel')}
              </Typography>
              <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" useFlexGap>
                {row.problemNodes > 0 ? (
                  <Chip
                    size="small"
                    color="error"
                    variant="outlined"
                    label={t('shareGroups.users.problemNodes', { count: row.problemNodes })}
                  />
                ) : (
                  <Typography variant="body2" color="text.secondary">
                    —
                  </Typography>
                )}
                {row.lastSeenAt && (
                  <Typography variant="caption" color="text.secondary">
                    {formatDateTime(row.lastSeenAt, language)}
                  </Typography>
                )}
              </Stack>
            </Grid>
          </Grid>

          {row.shares.length > 0 && (
            <>
              <Divider sx={{ my: 1 }} />
              <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
                {row.shares.slice(0, 3).map((link) => (
                  <Chip
                    key={link.share_id}
                    size="small"
                    variant="outlined"
                    label={link.subscription_name || link.share_name || `#${link.share_id}`}
                  />
                ))}
                {row.shares.length > 3 && <Chip size="small" variant="outlined" label={`+${row.shares.length - 3}`} />}
              </Stack>
            </>
          )}
        </CardContent>
      </Card>
    </Grid>
  );

  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
        <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
          {t('shareGroups.users.subtitle')}
        </Typography>
        {truncated && (
          <Chip size="small" variant="outlined" color="warning" label={t('shareGroups.users.truncated', { count: USER_DEVICE_LIMIT })} />
        )}
      </Stack>

      {loading ? (
        <Stack alignItems="center" sx={{ py: 6 }}>
          <CircularProgress />
        </Stack>
      ) : groups.length === 0 ? (
        <Alert severity="info" icon={<DevicesIcon />}>
          {t('shareGroups.users.empty')}
        </Alert>
      ) : (
        <Grid container spacing={2}>
          {groups.map(renderCard)}
        </Grid>
      )}

      <ShareDeviceDetailDialog
        open={Boolean(detailTarget)}
        hwid={detailTarget?.hwid}
        deviceId={detailTarget?.deviceId}
        days={days}
        onClose={() => setDetailTarget(null)}
      />
    </Box>
  );
}
