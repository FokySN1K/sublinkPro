import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';
import MenuItem from '@mui/material/MenuItem';
import Pagination from '@mui/material/Pagination';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getShareDevicesOverview } from 'api/shareGroups';

import ShareDeviceDetailDialog from './ShareDeviceDetailDialog';

const PAGE_SIZE = 20;

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

function subscriptionNames(item) {
  const names = new Set();
  for (const link of item.shares || []) {
    names.add(link.subscription_name || `#${link.subscription_id}`);
  }
  return Array.from(names);
}

// Devices tab: one row per installation (HWID + device id) aggregated across
// the subscription links it fetched.
export default function ShareDevicesPanel({ subId, days, showMessage, refreshKey }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState({ items: [], total: 0, summary: null, window_days: 7 });
  const [detailTarget, setDetailTarget] = useState(null);

  const windowDays = data.window_days || days || 7;

  const fetchDevices = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getShareDevicesOverview({
        subId: subId || undefined,
        status: status || undefined,
        keyword: keyword.trim() || undefined,
        days,
        page,
        pageSize: PAGE_SIZE
      });
      setData(res.data || { items: [], total: 0, summary: null, window_days: days });
    } catch (error) {
      setData({ items: [], total: 0, summary: null, window_days: days });
      showMessage?.(error.response?.data?.msg || t('shareGroups.telemetry.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [subId, status, keyword, days, page, showMessage, t]);

  // Debounce typing so the overview query does not fire on every keystroke.
  useEffect(() => {
    const timer = setTimeout(fetchDevices, 250);
    return () => clearTimeout(timer);
  }, [fetchDevices, refreshKey]);

  const handleFilterChange = useCallback((setter) => {
    return (event) => {
      setter(event.target.value);
      setPage(1);
    };
  }, []);

  const summaryChips = useMemo(() => {
    const summary = data.summary || {};
    const total = summary.total || 0;
    if (total === 0) return [];
    const chips = [{ key: 'total', label: t('shareGroups.telemetry.summary.total', { count: total }) }];
    if (summary.active) {
      chips.push({ key: 'active', label: t('shareGroups.telemetry.summary.active', { count: summary.active }), color: 'success' });
    }
    if (summary.pending) {
      chips.push({ key: 'pending', label: t('shareGroups.telemetry.summary.pending', { count: summary.pending }), color: 'warning' });
    }
    if (summary.blocked) {
      chips.push({ key: 'blocked', label: t('shareGroups.telemetry.summary.blocked', { count: summary.blocked }), color: 'error' });
    }
    chips.push({
      key: 'window',
      label: t('shareGroups.telemetry.summary.windowTraffic', {
        bytes: formatBytes(summary.window_traffic_bytes || 0),
        days: windowDays
      }),
      color: 'primary'
    });
    chips.push({
      key: 'lifetime',
      label: t('shareGroups.telemetry.summary.lifetimeTraffic', { bytes: formatBytes(summary.reported_traffic_bytes || 0) })
    });
    return chips;
  }, [data.summary, windowDays, t]);

  const openDetail = useCallback((item) => {
    setDetailTarget({ hwid: item.hwid, deviceId: item.device_id });
  }, []);

  const renderSubscriptions = (item) => (
    <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
      {subscriptionNames(item).map((name) => (
        <Chip key={name} size="small" label={name} variant="outlined" />
      ))}
      <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
        {t('shareGroups.telemetry.shareCount', { count: item.share_count || 0 })}
      </Typography>
    </Stack>
  );

  const renderTraffic = (item) => (
    <Box>
      <Typography variant="body2">{formatBytes(item.reported_traffic_bytes || 0)}</Typography>
      <Typography variant="caption" color="text.secondary" display="block">
        {t('shareGroups.telemetry.columns.windowTraffic', { days: windowDays })}: {formatBytes(item.window_traffic_bytes || 0)}
      </Typography>
    </Box>
  );

  return (
    <Box>
      <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} sx={{ mb: 2 }}>
        <TextField
          size="small"
          value={keyword}
          onChange={handleFilterChange(setKeyword)}
          placeholder={t('shareGroups.telemetry.filters.keywordPlaceholder')}
          sx={{ minWidth: { md: 260 } }}
        />
        <TextField
          select
          size="small"
          label={t('shareGroups.telemetry.filters.status')}
          value={status}
          onChange={handleFilterChange(setStatus)}
          sx={{ minWidth: { md: 180 } }}
        >
          <MenuItem value="">{t('shareGroups.telemetry.filters.statusAll')}</MenuItem>
          {['active', 'pending', 'blocked'].map((value) => (
            <MenuItem key={value} value={value}>
              {t(`shareGroups.deviceStatus.${value}`)}
            </MenuItem>
          ))}
        </TextField>
      </Stack>

      {summaryChips.length > 0 && (
        <Stack direction="row" spacing={0.75} flexWrap="wrap" useFlexGap sx={{ mb: 2 }}>
          {summaryChips.map((chip) => (
            <Chip
              key={chip.key}
              size="small"
              color={chip.color || 'default'}
              variant={chip.color ? 'outlined' : 'filled'}
              label={chip.label}
            />
          ))}
        </Stack>
      )}

      {loading ? (
        <Stack alignItems="center" sx={{ py: 6 }}>
          <CircularProgress />
        </Stack>
      ) : data.items.length === 0 ? (
        <Alert severity="info">{t('shareGroups.telemetry.empty')}</Alert>
      ) : isMobile ? (
        <Stack spacing={1.5}>
          {data.items.map((item) => (
            <Card
              key={`${item.hwid}-${item.device_id}`}
              variant="outlined"
              onClick={() => openDetail(item)}
              sx={{ p: 1.5, cursor: 'pointer' }}
            >
              <Stack direction="row" alignItems="flex-start" spacing={1}>
                <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                  <Stack direction="row" spacing={0.75} alignItems="center" flexWrap="wrap" useFlexGap>
                    <Typography variant="subtitle2" fontWeight={700} noWrap>
                      {item.name || t('shareGroups.telemetry.unknownDevice')}
                    </Typography>
                    <Chip
                      size="small"
                      color={statusColor(item.status)}
                      variant="outlined"
                      label={t(`shareGroups.deviceStatus.${item.status || 'active'}`)}
                    />
                  </Stack>
                  <Typography variant="caption" color="text.secondary" display="block" noWrap>
                    {[item.platform, item.app_version, item.device_id].filter(Boolean).join(' · ')}
                  </Typography>
                </Box>
                <ChevronRightIcon fontSize="small" color="action" />
              </Stack>
              <Box sx={{ mt: 1 }}>{renderSubscriptions(item)}</Box>
              <Divider sx={{ my: 1 }} />
              <Stack direction="row" justifyContent="space-between" spacing={1}>
                {renderTraffic(item)}
                <Box sx={{ textAlign: 'right' }}>
                  <Typography variant="caption" color="text.secondary" display="block">
                    {t('shareGroups.telemetry.columns.lastSeen')}
                  </Typography>
                  <Typography variant="caption">{item.last_seen_at ? formatDateTime(item.last_seen_at, language) : '-'}</Typography>
                </Box>
              </Stack>
            </Card>
          ))}
        </Stack>
      ) : (
        <TableContainer component={Card} variant="outlined">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('shareGroups.telemetry.columns.device')}</TableCell>
                <TableCell>{t('shareGroups.telemetry.columns.status')}</TableCell>
                <TableCell>{t('shareGroups.telemetry.columns.subscriptions')}</TableCell>
                <TableCell>{t('shareGroups.telemetry.columns.traffic')}</TableCell>
                <TableCell>{t('shareGroups.telemetry.columns.lastSeen')}</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {data.items.map((item) => (
                <TableRow key={`${item.hwid}-${item.device_id}`} hover sx={{ cursor: 'pointer' }} onClick={() => openDetail(item)}>
                  <TableCell>
                    <Typography variant="body2" fontWeight={600} noWrap>
                      {item.name || t('shareGroups.telemetry.unknownDevice')}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" display="block" noWrap>
                      {[item.platform, item.app_version].filter(Boolean).join(' · ') || item.device_id}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" display="block" noWrap sx={{ fontFamily: 'monospace' }}>
                      {item.hwid}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" useFlexGap>
                      <Chip
                        size="small"
                        color={statusColor(item.status)}
                        variant="outlined"
                        label={t(`shareGroups.deviceStatus.${item.status || 'active'}`)}
                      />
                      {item.blocked_count > 0 && <Chip size="small" color="error" variant="outlined" label={`${item.blocked_count}`} />}
                    </Stack>
                  </TableCell>
                  <TableCell>{renderSubscriptions(item)}</TableCell>
                  <TableCell>{renderTraffic(item)}</TableCell>
                  <TableCell>
                    <Typography variant="body2" noWrap>
                      {item.last_seen_at ? formatDateTime(item.last_seen_at, language) : '-'}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" display="block" noWrap>
                      {t('shareGroups.telemetry.columns.lastReport')}:{' '}
                      {item.last_report_at ? formatDateTime(item.last_report_at, language) : '-'}
                    </Typography>
                  </TableCell>
                  <TableCell padding="none">
                    <ChevronRightIcon fontSize="small" color="action" />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      {data.total > PAGE_SIZE && (
        <Stack alignItems="center" sx={{ mt: 2 }}>
          <Pagination
            page={page}
            count={Math.max(Math.ceil(data.total / PAGE_SIZE), 1)}
            onChange={(_, value) => setPage(value)}
            color="primary"
            size={isMobile ? 'small' : 'medium'}
          />
        </Stack>
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
