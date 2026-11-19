import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';

export function formatDuration(seconds) {
  const value = Number(seconds) || 0;
  if (value <= 0) return '0m';
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${Math.round(value)}s`;
}

function dayLabel(day) {
  const text = String(day || '');
  return text.length >= 10 ? text.slice(5) : text;
}

// Daily bars come pre-folded by the server into the local calendar of the
// server process. The tooltip shows the full day so users can verify dates.
export function ShareDailyBarChart({ daily = [], height = 120 }) {
  const { t } = useTranslation();
  const maxBytes = useMemo(() => Math.max(...daily.map((bucket) => bucket.bytes || 0), 1), [daily]);
  if (!daily.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        {t('shareGroups.blocks.noDaily')}
      </Typography>
    );
  }
  const labelIndexes = [0, Math.floor((daily.length - 1) / 2), daily.length - 1];
  return (
    <Box>
      <Stack direction="row" spacing={0.5} alignItems="flex-end" sx={{ height }}>
        {daily.map((bucket) => (
          <Tooltip key={bucket.day} title={`${bucket.day} — ${formatBytes(bucket.bytes || 0)}`} arrow>
            <Box sx={{ flex: 1, height: '100%', display: 'flex', alignItems: 'flex-end' }}>
              <Box
                sx={{
                  width: '100%',
                  minHeight: 2,
                  height: (bucket.bytes || 0) > 0 ? `${Math.max(((bucket.bytes || 0) / maxBytes) * 100, 4)}%` : '2px',
                  borderRadius: 0.5,
                  bgcolor: (bucket.bytes || 0) > 0 ? 'primary.main' : 'action.disabled',
                  opacity: (bucket.bytes || 0) > 0 ? 1 : 0.35
                }}
              />
            </Box>
          </Tooltip>
        ))}
      </Stack>
      <Stack direction="row" justifyContent="space-between" sx={{ mt: 0.5 }}>
        {labelIndexes.map((index, position) => (
          <Typography key={`${daily[index]?.day}-${position}`} variant="caption" color="text.secondary">
            {index === position * (daily.length - 1) ? dayLabel(daily[index]?.day) : ''}
          </Typography>
        ))}
      </Stack>
    </Box>
  );
}

export function ShareSessionsTable({ sessions = [], showShare = false, showDevice = false }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;
  if (!sessions.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        {t('shareGroups.blocks.noSessions')}
      </Typography>
    );
  }
  return (
    <Stack spacing={1}>
      {sessions.map((session) => (
        <Box
          key={`${session.share_id || ''}-${session.id}`}
          sx={{ p: 1.25, border: '1px solid', borderColor: 'divider', borderRadius: 1.5 }}
        >
          <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
            <Typography variant="body2" fontWeight={600}>
              {formatDateTime(session.started_at, language)}
            </Typography>
            {session.open ? (
              <Chip size="small" color="success" variant="outlined" label={t('shareGroups.blocks.sessionOpen')} />
            ) : (
              <Typography variant="caption" color="text.secondary">
                → {session.ended_at ? formatDateTime(session.ended_at, language) : '-'}
              </Typography>
            )}
            <Box sx={{ flexGrow: 1 }} />
            <Typography variant="body2">{formatBytes(session.bytes || 0)}</Typography>
          </Stack>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.25 }}>
            {formatDuration(session.duration_s)}
            {showDevice && session.device_name ? ` · ${session.device_name}` : ''}
            {showShare && session.share_name ? ` · ${session.share_name}` : ''}
            {session.session_key ? ` · ${session.session_key}` : ''}
          </Typography>
        </Box>
      ))}
    </Stack>
  );
}

function TrafficList({ title, rows, renderLabel, renderMeta, emptyText }) {
  if (!rows.length) {
    return (
      <Box>
        <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
          {title}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {emptyText || '—'}
        </Typography>
      </Box>
    );
  }
  return (
    <Box>
      <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
        {title}
      </Typography>
      <Stack spacing={0.75}>
        {rows.map((row) => (
          <Stack key={row.key} direction="row" spacing={1} alignItems="center">
            <Typography variant="body2" noWrap sx={{ minWidth: 0, flexGrow: 1 }}>
              {renderLabel(row)}
            </Typography>
            {renderMeta ? (
              <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
                {renderMeta(row)}
              </Typography>
            ) : null}
            <Typography variant="body2" sx={{ flexShrink: 0 }}>
              {formatBytes(row.bytes || 0)}
            </Typography>
          </Stack>
        ))}
      </Stack>
    </Box>
  );
}

export function ShareTrafficBreakdown({ nodes = [], airports = [], nodeLimit = 10 }) {
  const { t } = useTranslation();
  const airportRows = useMemo(() => airports.map((row) => ({ key: `${row.airport_id}`, ...row })), [airports]);
  const nodeRows = useMemo(
    () =>
      nodes.slice(0, nodeLimit).map((row) => ({
        key: row.key,
        ...row
      })),
    [nodes, nodeLimit]
  );
  return (
    <Stack direction={{ xs: 'column', md: 'row' }} spacing={3}>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <TrafficList
          title={t('shareGroups.blocks.airports')}
          rows={airportRows}
          renderLabel={(row) => row.airport_name || t('shareGroups.blocks.airportUnknown')}
          renderMeta={(row) => (row.share ? `${Math.round(row.share * 100)}%` : '')}
        />
      </Box>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <TrafficList
          title={t('shareGroups.blocks.nodes')}
          rows={nodeRows}
          renderLabel={(row) => row.name || row.key}
          renderMeta={(row) => row.airport_name || row.group || ''}
          emptyText={t('shareGroups.blocks.noNodes')}
        />
      </Box>
    </Stack>
  );
}

function reliabilityColor(state) {
  switch (state) {
    case 'suspect':
      return 'error';
    case 'silent':
      return 'warning';
    case 'ok':
      return 'success';
    default:
      return 'default';
  }
}

export function ShareReliabilityList({ reliability = [], events = [] }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;
  return (
    <Stack spacing={2}>
      <Box>
        <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
          {t('shareGroups.blocks.reliability')}
        </Typography>
        {reliability.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t('shareGroups.blocks.reliabilityEmpty')}
          </Typography>
        ) : (
          <Stack spacing={0.75}>
            {reliability.map((row) => (
              <Stack key={row.key} direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                <Chip
                  size="small"
                  color={reliabilityColor(row.state)}
                  variant="outlined"
                  label={t(`shareGroups.blocks.reliabilityStates.${row.state || 'neutral'}`)}
                />
                <Typography variant="body2" noWrap sx={{ minWidth: 0, flexGrow: 1 }}>
                  {row.name || row.key}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.blocks.reliabilityScore', { score: row.score })}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.blocks.reliabilityFails', { fails: row.fails, streak: row.streak })}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {row.last_seen_at ? formatDateTime(row.last_seen_at, language) : '-'}
                </Typography>
              </Stack>
            ))}
          </Stack>
        )}
      </Box>
      <Box>
        <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
          {t('shareGroups.blocks.reliabilityEvents')}
        </Typography>
        {events.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {t('shareGroups.blocks.reliabilityEventsEmpty')}
          </Typography>
        ) : (
          <Stack spacing={0.75}>
            {events.map((event) => (
              <Stack key={event.id} direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                <Chip
                  size="small"
                  variant="outlined"
                  label={t(`shareGroups.blocks.reasons.${event.reason}`, { defaultValue: event.reason || '-' })}
                />
                <Typography variant="body2" noWrap sx={{ minWidth: 0, flexGrow: 1 }}>
                  {event.node_name || event.node_key}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {event.from_state || '—'} → {event.to_state}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {formatDateTime(event.created_at, language)}
                </Typography>
              </Stack>
            ))}
          </Stack>
        )}
      </Box>
    </Stack>
  );
}
