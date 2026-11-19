import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';
import Grid from '@mui/material/Grid';
import IconButton from '@mui/material/IconButton';
import Stack from '@mui/material/Stack';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

import AddIcon from '@mui/icons-material/Add';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import PersonIcon from '@mui/icons-material/Person';

import { formatBytes } from 'views/airports/utils';
import { deleteShareClient, getShareClients, getShareSubscriptionsOverview } from 'api/shareGroups';

import ShareClientDialog from './ShareClientDialog';
import ShareUserDetailDialog from './ShareUserDetailDialog';

const USER_SHARE_LIMIT = 200;

// Users tab: subscription cards grouped by their owning client. Cards without
// a client are shown in a separate read-only "no user" group.
export default function ShareUsersPanel({ subId, days, showMessage, refreshKey }) {
  const { t } = useTranslation();

  const [loading, setLoading] = useState(false);
  const [clients, setClients] = useState([]);
  const [items, setItems] = useState([]);
  const [truncated, setTruncated] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingClient, setEditingClient] = useState(null);
  const [detailGroup, setDetailGroup] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const [clientsRes, overviewRes] = await Promise.all([
        getShareClients(),
        getShareSubscriptionsOverview({ subId: subId || undefined, days, page: 1, pageSize: USER_SHARE_LIMIT })
      ]);
      setClients(clientsRes.data?.items || []);
      const data = overviewRes.data || {};
      setItems(data.items || []);
      setTruncated((data.total || 0) > USER_SHARE_LIMIT);
    } catch (error) {
      setClients([]);
      setItems([]);
      showMessage?.(error.response?.data?.msg || t('shareGroups.users.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [subId, days, showMessage, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData, refreshKey]);

  const groups = useMemo(() => {
    const byClient = new Map();
    const unassigned = [];
    for (const item of items) {
      if (item.client_id) {
        const key = `${item.client_id}`;
        if (!byClient.has(key)) byClient.set(key, []);
        byClient.get(key).push(item);
      } else {
        unassigned.push(item);
      }
    }
    const summarize = (list) => ({
      shares: list.length,
      devices: list.reduce((sum, item) => sum + (item.devices?.total || 0), 0),
      online: list.reduce((sum, item) => sum + (item.online_now ? 1 : 0), 0),
      windowBytes: list.reduce((sum, item) => sum + (item.window_bytes || 0), 0),
      lifetimeBytes: list.reduce((sum, item) => sum + (item.lifetime_bytes || 0), 0),
      problemNodes: list.reduce((sum, item) => sum + (item.problem_nodes || 0), 0),
      sessionsToday: list.reduce((sum, item) => sum + (item.sessions_today || 0), 0)
    });
    const rows = clients.map((client) => {
      const list = byClient.get(`${client.id}`) || [];
      return { key: `client-${client.id}`, client, items: list, summary: summarize(list) };
    });
    rows.sort((left, right) => {
      if (left.summary.online !== right.summary.online) return right.summary.online - left.summary.online;
      if (left.summary.windowBytes !== right.summary.windowBytes) return right.summary.windowBytes - left.summary.windowBytes;
      return (left.client.name || '').localeCompare(right.client.name || '');
    });
    if (unassigned.length > 0) {
      rows.push({ key: 'unassigned', client: null, items: unassigned, summary: summarize(unassigned) });
    }
    return rows;
  }, [clients, items]);

  const handleDelete = useCallback(
    async (row) => {
      if (!row.client) return;
      if (!window.confirm(t('shareGroups.users.deleteConfirm', { name: row.client.name, count: row.items.length }))) return;
      try {
        await deleteShareClient(row.client.id);
        showMessage?.(t('shareGroups.users.deleteSuccess'));
        fetchData();
      } catch (error) {
        showMessage?.(error.response?.data?.msg || t('shareGroups.messages.deleteFailed'), 'error');
      }
    },
    [t, showMessage, fetchData]
  );

  const renderCard = (row) => {
    const { client, summary } = row;
    return (
      <Grid key={row.key} size={{ xs: 12, md: 6, xl: 4 }}>
        <Card
          sx={{ height: '100%', display: 'flex', flexDirection: 'column', border: '1px solid', borderColor: 'divider', cursor: 'pointer' }}
          onClick={() => setDetailGroup(row)}
        >
          <CardContent sx={{ flexGrow: 1 }}>
            <Stack direction="row" alignItems="flex-start" spacing={1}>
              <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
                  <Typography variant="subtitle1" fontWeight={700} noWrap>
                    {client ? client.name : t('shareGroups.users.unassigned')}
                  </Typography>
                  {client && (
                    <Chip
                      size="small"
                      label={client.enabled ? t('common.enabled') : t('common.disabled')}
                      color={client.enabled ? 'success' : 'default'}
                      variant="outlined"
                    />
                  )}
                  <Chip size="small" variant="outlined" label={t('shareGroups.users.shareCount', { count: summary.shares })} />
                </Stack>
                <Typography variant="caption" color="text.secondary" noWrap display="block">
                  {client ? client.note || '—' : t('shareGroups.users.unassignedHint')}
                </Typography>
              </Box>
              {client && (
                <Stack direction="row" spacing={0.25} onClick={(event) => event.stopPropagation()}>
                  <Tooltip title={t('common.edit')}>
                    <IconButton
                      size="small"
                      onClick={() => {
                        setEditingClient(client);
                        setDialogOpen(true);
                      }}
                    >
                      <EditIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title={t('common.delete')}>
                    <IconButton size="small" color="error" onClick={() => handleDelete(row)}>
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </Stack>
              )}
            </Stack>

            <Divider sx={{ my: 1.5 }} />

            <Grid container spacing={1}>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.devicesLabel')}
                </Typography>
                <Typography variant="body2">{t('shareGroups.users.devices', { count: summary.devices })}</Typography>
              </Grid>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.onlineLabel')}
                </Typography>
                <Typography variant="body2">{t('shareGroups.users.online', { count: summary.online })}</Typography>
              </Grid>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.windowLabel', { days })}
                </Typography>
                <Typography variant="body2">{formatBytes(summary.windowBytes)}</Typography>
              </Grid>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.sessionsLabel')}
                </Typography>
                <Typography variant="body2">{t('shareGroups.users.sessionsToday', { count: summary.sessionsToday })}</Typography>
              </Grid>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.lifetimeLabel')}
                </Typography>
                <Typography variant="body2">{formatBytes(summary.lifetimeBytes)}</Typography>
              </Grid>
              <Grid size={6}>
                <Typography variant="caption" color="text.secondary">
                  {t('shareGroups.users.problemsLabel')}
                </Typography>
                {summary.problemNodes > 0 ? (
                  <Chip
                    size="small"
                    color="error"
                    variant="outlined"
                    label={t('shareGroups.users.problemNodes', { count: summary.problemNodes })}
                  />
                ) : (
                  <Typography variant="body2">—</Typography>
                )}
              </Grid>
            </Grid>
          </CardContent>
        </Card>
      </Grid>
    );
  };

  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
        <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
          {t('shareGroups.users.subtitle')}
        </Typography>
        {truncated && (
          <Chip size="small" variant="outlined" color="warning" label={t('shareGroups.users.truncated', { count: USER_SHARE_LIMIT })} />
        )}
        <Button
          size="small"
          variant="contained"
          startIcon={<AddIcon />}
          onClick={() => {
            setEditingClient(null);
            setDialogOpen(true);
          }}
        >
          {t('shareGroups.users.add')}
        </Button>
      </Stack>

      {loading ? (
        <Stack alignItems="center" sx={{ py: 6 }}>
          <CircularProgress />
        </Stack>
      ) : groups.length === 0 ? (
        <Alert severity="info" icon={<PersonIcon />}>
          {t('shareGroups.users.empty')}
        </Alert>
      ) : (
        <Grid container spacing={2}>
          {groups.map(renderCard)}
        </Grid>
      )}

      <ShareClientDialog
        open={dialogOpen}
        client={editingClient}
        onClose={() => setDialogOpen(false)}
        onSaved={fetchData}
        showMessage={showMessage}
      />
      <ShareUserDetailDialog open={Boolean(detailGroup)} group={detailGroup} days={days} onClose={() => setDetailGroup(null)} />
    </Box>
  );
}
