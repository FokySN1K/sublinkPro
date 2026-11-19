import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Card from '@mui/material/Card';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';
import LinearProgress from '@mui/material/LinearProgress';
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
import { getShareSubscriptionsOverview } from 'api/shareGroups';

import ShareSubscriptionDetailDialog from './ShareSubscriptionDetailDialog';

const PAGE_SIZE = 20;

function statusColor(status) {
  switch (status) {
    case 'enabled':
      return 'success';
    case 'expired':
      return 'warning';
    case 'disabled':
      return 'default';
    default:
      return 'default';
  }
}

function QuotaCell({ item }) {
  const { t } = useTranslation();
  const limit = item.traffic_limit_bytes || 0;
  const used = item.reported_traffic_bytes || 0;
  if (limit <= 0) {
    return (
      <Box>
        <Typography variant="body2">{formatBytes(used)}</Typography>
        <Typography variant="caption" color="text.secondary" display="block">
          {t('shareGroups.subscriptions.quota.unlimited')}
        </Typography>
      </Box>
    );
  }
  const percent = Math.min(Math.round((used / limit) * 1000) / 10, 100);
  return (
    <Box sx={{ minWidth: 140 }}>
      <LinearProgress
        variant="determinate"
        value={percent}
        color={item.quota_exhausted ? 'error' : percent >= 80 ? 'warning' : 'primary'}
        sx={{ height: 6, borderRadius: 3, mb: 0.5 }}
      />
      <Typography variant="caption" color="text.secondary" display="block">
        {formatBytes(used)} / {formatBytes(limit)}
      </Typography>
      <Typography variant="caption" color={item.quota_exhausted ? 'error.main' : 'text.secondary'} display="block">
        {item.quota_exhausted
          ? t('shareGroups.subscriptions.quota.exhausted')
          : t('shareGroups.subscriptions.quota.remaining', { bytes: formatBytes(item.remaining_bytes || 0) })}
      </Typography>
    </Box>
  );
}

function DeviceCounts({ counts }) {
  const { t } = useTranslation();
  return (
    <Box>
      <Typography variant="body2">
        {t('shareGroups.subscriptions.deviceCounts', { total: counts?.total || 0, active: counts?.active || 0 })}
      </Typography>
      <Stack direction="row" spacing={0.5} sx={{ mt: 0.25 }} flexWrap="wrap" useFlexGap>
        {counts?.pending > 0 && <Chip size="small" color="warning" variant="outlined" label={t('shareGroups.deviceStatus.pending')} />}
        {counts?.blocked > 0 && <Chip size="small" color="error" variant="outlined" label={t('shareGroups.deviceStatus.blocked')} />}
      </Stack>
    </Box>
  );
}

function SessionsCell({ item }) {
  const { t } = useTranslation();
  return (
    <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" useFlexGap>
      {item.online_now && <Chip size="small" color="success" variant="outlined" label={t('shareGroups.subscriptions.online')} />}
      <Typography variant="caption" color="text.secondary">
        {t('shareGroups.subscriptions.sessionsToday', { count: item.sessions_today || 0 })}
      </Typography>
      {item.last_session && (
        <Typography variant="caption" color="text.secondary" display="block">
          {formatDateTime(item.last_session.started_at)}
        </Typography>
      )}
    </Stack>
  );
}

// Subscriptions tab: one row per share link with quota, devices and traffic.
export default function ShareSubscriptionsPanel({ subId, days, showMessage, refreshKey }) {
  const { t, i18n } = useTranslation();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('md'));
  const language = i18n.resolvedLanguage || i18n.language;

  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState({ items: [], total: 0, summary: null, window_days: days });
  const [detailTarget, setDetailTarget] = useState(null);

  const windowDays = data.window_days || days || 7;

  const fetchRows = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getShareSubscriptionsOverview({
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
      showMessage?.(error.response?.data?.msg || t('shareGroups.subscriptions.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [subId, status, keyword, days, page, showMessage, t]);

  useEffect(() => {
    const timer = setTimeout(fetchRows, 250);
    return () => clearTimeout(timer);
  }, [fetchRows, refreshKey]);

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
    const chips = [{ key: 'total', label: t('shareGroups.subscriptions.summary.total', { count: total }) }];
    if (summary.enabled)
      chips.push({ key: 'enabled', label: t('shareGroups.subscriptions.summary.enabled', { count: summary.enabled }), color: 'success' });
    if (summary.expired)
      chips.push({ key: 'expired', label: t('shareGroups.subscriptions.summary.expired', { count: summary.expired }), color: 'warning' });
    if (summary.disabled)
      chips.push({ key: 'disabled', label: t('shareGroups.subscriptions.summary.disabled', { count: summary.disabled }) });
    if (summary.online)
      chips.push({ key: 'online', label: t('shareGroups.subscriptions.summary.online', { count: summary.online }), color: 'success' });
    chips.push({ key: 'devices', label: t('shareGroups.subscriptions.summary.devices', { count: summary.devices || 0 }) });
    chips.push({
      key: 'window',
      label: t('shareGroups.subscriptions.summary.windowTraffic', {
        bytes: formatBytes(summary.window_traffic_bytes || 0),
        days: windowDays
      }),
      color: 'primary'
    });
    if (summary.problem_nodes > 0) {
      chips.push({
        key: 'problems',
        label: t('shareGroups.subscriptions.summary.problemNodes', { count: summary.problem_nodes }),
        color: 'error'
      });
    }
    return chips;
  }, [data.summary, windowDays, t]);

  const openDetail = useCallback((item) => {
    setDetailTarget(item.share_id);
  }, []);

  const renderIdentity = (item) => (
    <Box sx={{ minWidth: 0 }}>
      <Stack direction="row" spacing={0.75} alignItems="center" flexWrap="wrap" useFlexGap>
        <Typography variant="body2" fontWeight={600} noWrap>
          {item.share_name || `#${item.share_id}`}
        </Typography>
        <Chip
          size="small"
          color={statusColor(item.status)}
          variant="outlined"
          label={t(`shareGroups.subscriptions.status.${item.status || 'enabled'}`)}
        />
        {item.quota_exhausted && (
          <Chip size="small" color="error" variant="outlined" label={t('shareGroups.subscriptions.quota.exhausted')} />
        )}
      </Stack>
      <Typography variant="caption" color="text.secondary" display="block" noWrap>
        {[item.subscription_name || `#${item.subscription_id}`, item.group_name].filter(Boolean).join(' · ')}
      </Typography>
      <Typography variant="caption" color="text.secondary" display="block" noWrap sx={{ fontFamily: 'monospace' }}>
        {item.token}
      </Typography>
    </Box>
  );

  const renderClient = (item) => (
    <Chip
      size="small"
      variant="outlined"
      color={item.client_id ? 'primary' : 'default'}
      label={item.client_name || t('shareGroups.subscriptions.noClient')}
    />
  );

  const renderTraffic = (item) => (
    <Box>
      <Typography variant="body2">{formatBytes(item.window_bytes || 0)}</Typography>
      <Typography variant="caption" color="text.secondary" display="block">
        {t('shareGroups.subscriptions.today', { bytes: formatBytes(item.today_bytes || 0) })}
      </Typography>
      <Typography variant="caption" color="text.secondary" display="block">
        {t('shareGroups.subscriptions.lifetime', { bytes: formatBytes(item.lifetime_bytes || 0) })}
      </Typography>
    </Box>
  );

  const renderProblems = (item) =>
    item.problem_nodes > 0 ? (
      <Chip
        size="small"
        color="error"
        variant="outlined"
        label={t('shareGroups.subscriptions.problemChip', { count: item.problem_nodes })}
      />
    ) : (
      <Typography variant="caption" color="text.secondary">
        —
      </Typography>
    );

  const renderLastSeen = (item) => (
    <Box>
      <Typography variant="body2" noWrap>
        {item.last_seen_at ? formatDateTime(item.last_seen_at, language) : '-'}
      </Typography>
      <Typography variant="caption" color="text.secondary" display="block" noWrap>
        {t('shareGroups.telemetry.columns.lastReport')}: {item.last_report_at ? formatDateTime(item.last_report_at, language) : '-'}
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
          placeholder={t('shareGroups.subscriptions.searchPlaceholder')}
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
          {['enabled', 'expired', 'disabled'].map((value) => (
            <MenuItem key={value} value={value}>
              {t(`shareGroups.subscriptions.status.${value}`)}
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
        <Alert severity="info">{t('shareGroups.subscriptions.empty')}</Alert>
      ) : isMobile ? (
        <Stack spacing={1.5}>
          {data.items.map((item) => (
            <Card key={item.share_id} variant="outlined" onClick={() => openDetail(item)} sx={{ p: 1.5, cursor: 'pointer' }}>
              <Stack direction="row" alignItems="flex-start" spacing={1}>
                <Box sx={{ minWidth: 0, flexGrow: 1 }}>{renderIdentity(item)}</Box>
                <ChevronRightIcon fontSize="small" color="action" />
              </Stack>
              <Stack direction="row" spacing={0.75} sx={{ mt: 1 }} flexWrap="wrap" useFlexGap>
                {renderClient(item)}
                {renderProblems(item)}
              </Stack>
              <Divider sx={{ my: 1 }} />
              <Stack direction="row" justifyContent="space-between" spacing={1.5} alignItems="flex-start">
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <QuotaCell item={item} />
                </Box>
                {renderTraffic(item)}
              </Stack>
              <Divider sx={{ my: 1 }} />
              <Stack direction="row" justifyContent="space-between" spacing={1} alignItems="center">
                <DeviceCounts counts={item.devices} />
                <SessionsCell item={item} />
              </Stack>
            </Card>
          ))}
        </Stack>
      ) : (
        <TableContainer component={Card} variant="outlined">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('shareGroups.subscriptions.columns.share')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.client')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.quota')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.devices')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.traffic')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.sessions')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.problems')}</TableCell>
                <TableCell>{t('shareGroups.subscriptions.columns.lastReport')}</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {data.items.map((item) => (
                <TableRow key={item.share_id} hover sx={{ cursor: 'pointer' }} onClick={() => openDetail(item)}>
                  <TableCell>{renderIdentity(item)}</TableCell>
                  <TableCell>{renderClient(item)}</TableCell>
                  <TableCell>
                    <QuotaCell item={item} />
                  </TableCell>
                  <TableCell>
                    <DeviceCounts counts={item.devices} />
                  </TableCell>
                  <TableCell>{renderTraffic(item)}</TableCell>
                  <TableCell>
                    <SessionsCell item={item} />
                  </TableCell>
                  <TableCell>{renderProblems(item)}</TableCell>
                  <TableCell>{renderLastSeen(item)}</TableCell>
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

      <ShareSubscriptionDetailDialog
        open={Boolean(detailTarget)}
        shareId={detailTarget}
        days={days}
        onClose={() => setDetailTarget(null)}
        onChanged={fetchRows}
        showMessage={showMessage}
      />
    </Box>
  );
}
