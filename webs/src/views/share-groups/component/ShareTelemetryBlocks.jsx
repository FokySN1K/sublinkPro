import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import TableSortLabel from '@mui/material/TableSortLabel';
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

// VPN activity is folded server side so it stays independent from the traffic:
// it answers when the client turned on and off and how long it stayed up.
export function ShareSessionStats({ stats = null }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;
  if (!stats || !stats.sessions) return null;
  return (
    <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
      <Chip size="small" variant="outlined" label={t('shareGroups.blocks.sessionCount', { count: stats.sessions })} />
      <Chip
        size="small"
        variant="outlined"
        label={t('shareGroups.blocks.sessionActiveTime', { time: formatDuration(stats.active_seconds) })}
      />
      {stats.last_started_at && (
        <Chip
          size="small"
          variant="outlined"
          label={t('shareGroups.blocks.sessionLastOn', { time: formatDateTime(stats.last_started_at, language) })}
        />
      )}
      {stats.open_sessions > 0 ? (
        <Chip size="small" color="success" variant="outlined" label={t('shareGroups.blocks.sessionOpen')} />
      ) : (
        stats.last_ended_at && (
          <Chip
            size="small"
            variant="outlined"
            label={t('shareGroups.blocks.sessionLastOff', { time: formatDateTime(stats.last_ended_at, language) })}
          />
        )
      )}
      <Chip size="small" variant="outlined" label={t('shareGroups.blocks.sessionTraffic', { bytes: formatBytes(stats.bytes || 0) })} />
    </Stack>
  );
}

// The node table joins the per-node traffic of the window with the SNKP
// reputation snapshot by node key: one table answers both "how much did this
// node carry" and "was the pool happy with it". Sorting is client side because
// a detail page holds at most a few hundred nodes.
const NODE_STATE_RANK = { suspect: 0, silent: 1, neutral: 2, ok: 3 };

function nodeStateRank(state) {
  return NODE_STATE_RANK[state] ?? 4;
}

function compareNodeRows(left, right, column) {
  switch (column) {
    case 'name':
      return (left.name || left.key).localeCompare(right.name || right.key);
    case 'state':
      return nodeStateRank(left.state) - nodeStateRank(right.state);
    case 'score':
      return (left.score ?? -101) - (right.score ?? -101);
    case 'lastSeen':
      return (Date.parse(left.last_seen_at || 0) || 0) - (Date.parse(right.last_seen_at || 0) || 0);
    default:
      return (left.bytes || 0) - (right.bytes || 0);
  }
}

export function ShareNodeTelemetryTable({ nodes = [], reputation = [] }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;
  const [orderBy, setOrderBy] = useState('bytes');
  const [direction, setDirection] = useState('desc');

  const rows = useMemo(() => {
    const index = new Map();
    for (const node of nodes) {
      index.set(node.key, {
        key: node.key,
        name: node.name || '',
        airport_name: node.airport_name || '',
        bytes: node.bytes || 0,
        state: '',
        score: null,
        fails: 0,
        streak: 0,
        last_seen_at: null
      });
    }
    for (const entry of reputation) {
      const row = index.get(entry.key) || { key: entry.key, name: '', airport_name: '', bytes: 0, state: '', score: null };
      row.name = row.name || entry.name || '';
      row.airport_name = row.airport_name || entry.airport_name || '';
      // Each installation scores the same node, and the API lists problems
      // first: keep that first snapshot so the worst state wins.
      if (row.score === null) {
        row.state = entry.state || '';
        row.score = entry.score;
        row.fails = entry.fails || 0;
        row.streak = entry.streak || 0;
        row.last_seen_at = entry.last_seen_at;
      }
      index.set(entry.key, row);
    }
    const list = Array.from(index.values());
    const total = list.reduce((sum, row) => sum + (row.bytes || 0), 0);
    return { list, total };
  }, [nodes, reputation]);

  const sorted = useMemo(() => {
    const factor = direction === 'asc' ? 1 : -1;
    return [...rows.list].sort((left, right) => {
      const result = compareNodeRows(left, right, orderBy);
      if (result !== 0) return result * factor;
      return (left.name || left.key).localeCompare(right.name || right.key);
    });
  }, [rows, orderBy, direction]);

  if (rows.list.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        {t('shareGroups.blocks.noNodes')}
      </Typography>
    );
  }

  const handleSort = (column) => {
    if (column === orderBy) {
      setDirection((current) => (current === 'asc' ? 'desc' : 'asc'));
      return;
    }
    setOrderBy(column);
    setDirection(column === 'name' ? 'asc' : 'desc');
  };

  // Without a reputation snapshot the state columns would stay empty, so the
  // traffic view stays readable on installations that never reported one.
  const columns = [
    { id: 'name', label: t('shareGroups.blocks.tableNode') },
    ...(reputation.length > 0
      ? [
          { id: 'state', label: t('shareGroups.blocks.tableState') },
          { id: 'score', label: t('shareGroups.blocks.tableScore'), align: 'right' }
        ]
      : []),
    { id: 'bytes', label: t('shareGroups.blocks.tableTraffic'), align: 'right' },
    { id: 'lastSeen', label: t('shareGroups.blocks.tableLastSeen'), align: 'right' }
  ];

  return (
    <TableContainer sx={{ maxHeight: 380, border: '1px solid', borderColor: 'divider', borderRadius: 1.5 }}>
      <Table size="small" stickyHeader>
        <TableHead>
          <TableRow>
            {columns.map((column) => (
              <TableCell key={column.id} align={column.align || 'left'} sortDirection={orderBy === column.id ? direction : false}>
                <TableSortLabel
                  active={orderBy === column.id}
                  direction={orderBy === column.id ? direction : 'desc'}
                  onClick={() => handleSort(column.id)}
                >
                  {column.label}
                </TableSortLabel>
              </TableCell>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {sorted.map((row) => (
            <TableRow key={row.key} hover>
              <TableCell sx={{ minWidth: 160 }}>
                <Typography variant="body2" noWrap>
                  {row.name || row.key}
                </Typography>
                <Typography variant="caption" color="text.secondary" noWrap display="block">
                  {row.airport_name || row.key}
                </Typography>
              </TableCell>
              {reputation.length > 0 && (
                <TableCell>
                  {row.state ? (
                    <Chip
                      size="small"
                      variant="outlined"
                      color={reliabilityColor(row.state)}
                      label={t(`shareGroups.blocks.reliabilityStates.${row.state}`)}
                    />
                  ) : (
                    <Typography variant="caption" color="text.secondary">
                      —
                    </Typography>
                  )}
                </TableCell>
              )}
              {reputation.length > 0 && (
                <TableCell align="right">
                  <Typography variant="body2">{row.score === null ? '—' : row.score}</Typography>
                  {row.fails > 0 && (
                    <Typography variant="caption" color="text.secondary" noWrap display="block">
                      {t('shareGroups.blocks.reliabilityFails', { fails: row.fails, streak: row.streak })}
                    </Typography>
                  )}
                </TableCell>
              )}
              <TableCell align="right">
                <Typography variant="body2">{formatBytes(row.bytes || 0)}</Typography>
                {rows.total > 0 && (
                  <Typography variant="caption" color="text.secondary" display="block">
                    {Math.round(((row.bytes || 0) / rows.total) * 1000) / 10}%
                  </Typography>
                )}
              </TableCell>
              <TableCell align="right">
                <Typography variant="caption" color="text.secondary" noWrap>
                  {row.last_seen_at ? formatDateTime(row.last_seen_at, language) : '—'}
                </Typography>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
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

export function ShareTrafficBreakdown({ nodes = [], airports = [], reputation = [] }) {
  const { t } = useTranslation();
  const airportRows = useMemo(() => airports.map((row) => ({ key: `${row.airport_id}`, ...row })), [airports]);
  return (
    <Stack spacing={2.5}>
      <TrafficList
        title={t('shareGroups.blocks.airports')}
        rows={airportRows}
        renderLabel={(row) => row.airport_name || t('shareGroups.blocks.airportUnknown')}
        renderMeta={(row) => (row.share ? `${Math.round(row.share * 100)}%` : '')}
        emptyText="—"
      />
      <Box>
        <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 0.5 }}>
          {t('shareGroups.blocks.nodeTable')}
        </Typography>
        <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
          {t('shareGroups.blocks.nodeTableHint')}
        </Typography>
        <ShareNodeTelemetryTable nodes={nodes} reputation={reputation} />
      </Box>
    </Stack>
  );
}

// Reliable nodes are the healthy mirror of the problem list: the pool keeps
// marking them as working, so the analyst sees which nodes carry the load
// without failing. Traffic is joined from the per-node buckets by node key.
export function ShareReliableNodes({ reliable = [], nodes = [] }) {
  const { t, i18n } = useTranslation();
  const language = i18n.resolvedLanguage || i18n.language;
  const bytesByKey = useMemo(() => {
    const index = new Map();
    for (const node of nodes) {
      index.set(node.key, (index.get(node.key) || 0) + (node.bytes || 0));
    }
    return index;
  }, [nodes]);
  return (
    <Box>
      <Typography variant="subtitle2" fontWeight={700} sx={{ mb: 1 }}>
        {t('shareGroups.blocks.reliable')}
      </Typography>
      {reliable.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('shareGroups.blocks.reliableEmpty')}
        </Typography>
      ) : (
        <Stack spacing={0.75}>
          {reliable.map((row) => (
            <Stack key={row.key} direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
              <Chip
                size="small"
                color="success"
                variant="outlined"
                label={t('shareGroups.blocks.reliabilityScore', { score: row.score })}
              />
              <Typography variant="body2" noWrap sx={{ minWidth: 0, flexGrow: 1 }}>
                {row.name || row.key}
                {row.airport_name ? ` · ${row.airport_name}` : ''}
              </Typography>
              {bytesByKey.size > 0 && (
                <Typography variant="caption" color="text.secondary">
                  {formatBytes(bytesByKey.get(row.key) || 0)}
                </Typography>
              )}
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
