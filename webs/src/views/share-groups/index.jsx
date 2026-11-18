import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Box from '@mui/material/Box';
import Grid from '@mui/material/Grid';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Tabs from '@mui/material/Tabs';
import Tab from '@mui/material/Tab';
import Tooltip from '@mui/material/Tooltip';
import Alert from '@mui/material/Alert';
import Snackbar from '@mui/material/Snackbar';
import CircularProgress from '@mui/material/CircularProgress';
import Divider from '@mui/material/Divider';

import AddIcon from '@mui/icons-material/Add';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import RefreshIcon from '@mui/icons-material/Refresh';
import HistoryIcon from '@mui/icons-material/History';
import LayersIcon from '@mui/icons-material/Layers';
import LinkIcon from '@mui/icons-material/Link';
import NoteIcon from '@mui/icons-material/Note';
import InsightsIcon from '@mui/icons-material/Insights';

import MainCard from 'ui-component/cards/MainCard';
import { formatDateTime } from 'i18n/locales';
import { formatBytes } from 'views/airports/utils';
import { getSubscriptions } from 'api/subscriptions';
import { getShareGroups, deleteShareGroup, getShareStubs, deleteShareStub } from 'api/shareGroups';

import ShareManageDialog from './component/ShareManageDialog';
import ShareGroupDialog from './component/ShareGroupDialog';
import ShareGroupStubsDialog from './component/ShareGroupStubsDialog';
import ShareGroupActivityDialog from './component/ShareGroupActivityDialog';
import ShareStubDialog from './component/ShareStubDialog';
import ShareTelemetryTab from './component/ShareTelemetryTab';

const GB = 1024 * 1024 * 1024;

// Limit formatting keeps the "unlimited" glyph for zero values, unlike the
// shared byte formatter used for reported traffic.
function formatLimitBytes(value) {
  const bytes = Number(value) || 0;
  if (bytes <= 0) return '∞';
  if (bytes >= GB) return `${(bytes / GB).toFixed(2)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${bytes} B`;
}

export default function ShareGroupsPage() {
  const { t, i18n } = useTranslation();

  const [tab, setTab] = useState(0);
  const [loading, setLoading] = useState(false);
  const [groups, setGroups] = useState([]);
  const [stubs, setStubs] = useState([]);
  const [cases, setCases] = useState([]);
  const [pools, setPools] = useState([]);
  const [snackbar, setSnackbar] = useState({ open: false, message: '', severity: 'success' });

  const [groupDialogOpen, setGroupDialogOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState(null);
  const [stubsDialogOpen, setStubsDialogOpen] = useState(false);
  const [stubsGroup, setStubsGroup] = useState(null);
  const [activityOpen, setActivityOpen] = useState(false);
  const [activityGroup, setActivityGroup] = useState(null);
  const [stubDialogOpen, setStubDialogOpen] = useState(false);
  const [editingStub, setEditingStub] = useState(null);
  const [linksGroup, setLinksGroup] = useState(null);

  const showMessage = useCallback((message, severity = 'success') => {
    setSnackbar({ open: true, message, severity });
  }, []);

  const fetchGroups = useCallback(async () => {
    const res = await getShareGroups();
    setGroups(res.data || []);
  }, []);

  const fetchStubs = useCallback(async () => {
    const res = await getShareStubs();
    setStubs(res.data?.items || []);
    setCases(res.data?.cases || []);
  }, []);

  const fetchPools = useCallback(async () => {
    const res = await getSubscriptions({ page: 1, pageSize: 200, type: 'collection' });
    const items = res.data?.items || res.data || [];
    setPools(items);
  }, []);

  const loadAll = useCallback(
    async (showLoading = true) => {
      if (showLoading) setLoading(true);
      try {
        await Promise.all([fetchGroups(), fetchStubs(), fetchPools()]);
      } catch (error) {
        showMessage(error.message || t('shareGroups.messages.loadFailed'), 'error');
      } finally {
        if (showLoading) setLoading(false);
      }
    },
    [fetchGroups, fetchStubs, fetchPools, showMessage, t]
  );

  useEffect(() => {
    loadAll();
  }, [loadAll]);

  const handleDeleteGroup = useCallback(
    async (group) => {
      if (!window.confirm(t('shareGroups.confirm.deleteGroup', { name: group.name }))) return;
      try {
        await deleteShareGroup(group.id);
        showMessage(t('shareGroups.messages.deleteSuccess'));
        fetchGroups();
      } catch (error) {
        showMessage(error.response?.data?.msg || t('shareGroups.messages.deleteFailed'), 'error');
      }
    },
    [t, showMessage, fetchGroups]
  );

  const handleDeleteStub = useCallback(
    async (stub) => {
      if (!window.confirm(t('shareGroups.confirm.deleteStub', { name: stub.name || stub.title }))) return;
      try {
        await deleteShareStub(stub.id);
        showMessage(t('shareGroups.messages.stubDeleted'));
        fetchStubs();
      } catch (error) {
        showMessage(error.response?.data?.msg || t('shareGroups.messages.deleteFailed'), 'error');
      }
    },
    [t, showMessage, fetchStubs]
  );

  const stubName = useCallback(
    (stubId) => {
      const stub = stubs.find((item) => item.id === stubId);
      return stub?.name || stub?.title || `#${stubId}`;
    },
    [stubs]
  );

  const deviceModeLabel = useCallback((mode) => t(`shareGroups.deviceModes.${mode || 'open'}`), [t]);

  const groupCards = useMemo(
    () =>
      groups.map((group) => {
        const templateFiles = [
          group.clash_template ? `Clash: ${group.clash_template}` : '',
          group.surge_template ? `Surge: ${group.surge_template}` : ''
        ].filter(Boolean);

        return (
          <Grid key={group.id} size={{ xs: 12, md: 6, xl: 4 }}>
            <Card sx={{ height: '100%', display: 'flex', flexDirection: 'column', border: '1px solid', borderColor: 'divider' }}>
              <CardContent sx={{ flexGrow: 1, display: 'flex', flexDirection: 'column' }}>
                <Stack direction="row" alignItems="flex-start" spacing={1}>
                  <Box sx={{ minWidth: 0, flexGrow: 1 }}>
                    <Stack direction="row" alignItems="center" spacing={1} flexWrap="wrap" useFlexGap>
                      <Typography variant="subtitle1" fontWeight={700} noWrap>
                        {group.name}
                      </Typography>
                      <Chip
                        size="small"
                        label={group.enabled ? t('common.enabled') : t('common.disabled')}
                        color={group.enabled ? 'success' : 'default'}
                        variant="outlined"
                      />
                      {group.hide_config && (
                        <Chip size="small" label={t('shareGroups.badges.snkpOnly')} color="primary" variant="outlined" />
                      )}
                      {group.exhausted_share_count > 0 && (
                        <Chip
                          size="small"
                          label={t('shareGroups.badges.quotaExhaustedCount', { count: group.exhausted_share_count })}
                          color="error"
                          variant="outlined"
                        />
                      )}
                    </Stack>
                    <Typography variant="caption" color="text.secondary" noWrap display="block">
                      {group.pool_name || t('shareGroups.fields.poolUnknown')}
                    </Typography>
                  </Box>
                </Stack>

                <Stack direction="row" spacing={0.5} flexWrap="wrap" sx={{ mt: 1.5, gap: 0.5 }}>
                  {(group.template_list || []).map((template) => (
                    <Chip
                      key={template}
                      size="small"
                      label={t(`shareGroups.templates.${template}`)}
                      sx={{ bgcolor: 'action.hover', color: 'primary.main' }}
                    />
                  ))}
                </Stack>

                <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
                  {templateFiles.length > 0
                    ? templateFiles.join(' · ')
                    : `${t('shareGroups.fields.clashTemplate')}: ${t('shareGroups.fields.templateInherit')} · ${t(
                        'shareGroups.fields.surgeTemplate'
                      )}: ${t('shareGroups.fields.templateInherit')}`}
                </Typography>

                <Divider sx={{ my: 1.5 }} />

                <Grid container spacing={1}>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.deviceMode')}
                    </Typography>
                    <Typography variant="body2">{deviceModeLabel(group.device_mode)}</Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.maxDevices')}
                    </Typography>
                    <Typography variant="body2">{group.max_devices > 0 ? group.max_devices : '∞'}</Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.trafficLimit')}
                    </Typography>
                    <Typography variant="body2">{formatLimitBytes(group.traffic_limit_bytes)}</Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.reportedTraffic')}
                    </Typography>
                    <Typography variant="body2">
                      {formatBytes(group.reported_traffic_bytes || 0)} /{' '}
                      {group.traffic_limit_bytes > 0 ? formatBytes(group.traffic_limit_bytes) : '∞'}
                    </Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.telemetryEnabled')}
                    </Typography>
                    <Typography variant="body2">
                      {group.telemetry_enabled
                        ? `${group.report_interval_sessions}/${group.report_interval_nodes}/${group.report_interval_reliability}s`
                        : t('common.disabled')}
                    </Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.shareCount')}
                    </Typography>
                    <Typography variant="body2">{group.share_count || 0}</Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.deviceCount')}
                    </Typography>
                    <Typography variant="body2">
                      {group.device_count || 0}
                      {group.pending_device_count > 0 ? ` (+${group.pending_device_count})` : ''}
                    </Typography>
                  </Grid>
                  <Grid size={6}>
                    <Typography variant="caption" color="text.secondary">
                      {t('shareGroups.fields.lastReport')}
                    </Typography>
                    <Typography variant="body2">
                      {group.last_usage_at ? formatDateTime(group.last_usage_at, i18n.resolvedLanguage || i18n.language) : '-'}
                    </Typography>
                  </Grid>
                </Grid>

                <Typography variant="caption" color="text.secondary" sx={{ mt: 1.5, display: 'block' }}>
                  {t('shareGroups.fields.stubMapping')}:{' '}
                  {Object.keys(group.stubs || {}).length === 0
                    ? t('shareGroups.stubsDialog.useDefault')
                    : Object.entries(group.stubs)
                        .map(([caseKey, stubId]) => `${caseKey}→${stubName(stubId)}`)
                        .join(', ')}
                </Typography>
              </CardContent>

              <Divider />
              <Stack direction="row" alignItems="center" spacing={0.5} sx={{ px: 1.5, py: 0.75, flexWrap: 'wrap', rowGap: 0.5 }}>
                <Button size="small" startIcon={<LinkIcon />} onClick={() => setLinksGroup(group)}>
                  {t('shareGroups.actions.links')}
                </Button>
                <Button
                  size="small"
                  startIcon={<NoteIcon />}
                  onClick={() => {
                    setStubsGroup(group);
                    setStubsDialogOpen(true);
                  }}
                >
                  {t('shareGroups.actions.stubs')}
                </Button>
                <Button
                  size="small"
                  startIcon={<HistoryIcon />}
                  onClick={() => {
                    setActivityGroup(group);
                    setActivityOpen(true);
                  }}
                >
                  {t('shareGroups.actions.activity')}
                </Button>
                <Box sx={{ flexGrow: 1 }} />
                <Tooltip title={t('common.edit')}>
                  <IconButton
                    size="small"
                    onClick={() => {
                      setEditingGroup(group);
                      setGroupDialogOpen(true);
                    }}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title={t('common.delete')}>
                  <IconButton size="small" color="error" onClick={() => handleDeleteGroup(group)}>
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </Stack>
            </Card>
          </Grid>
        );
      }),
    [groups, t, i18n, deviceModeLabel, stubName, handleDeleteGroup]
  );

  const stubCards = useMemo(
    () =>
      stubs.map((stub) => (
        <Grid key={stub.id} size={{ xs: 12, md: 6, xl: 4 }}>
          <Card sx={{ height: '100%', border: '1px solid', borderColor: 'divider' }}>
            <CardContent>
              <Stack direction="row" alignItems="flex-start" justifyContent="space-between" spacing={1}>
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="subtitle1" fontWeight={700} noWrap>
                    {stub.name || stub.title}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {stub.case_key}
                  </Typography>
                </Box>
                <Stack direction="row" spacing={0.5}>
                  <Chip
                    size="small"
                    label={stub.enabled ? t('common.enabled') : t('common.disabled')}
                    color={stub.enabled ? 'success' : 'default'}
                    variant="outlined"
                  />
                  <IconButton
                    size="small"
                    onClick={() => {
                      setEditingStub(stub);
                      setStubDialogOpen(true);
                    }}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                  <IconButton size="small" color="error" onClick={() => handleDeleteStub(stub)}>
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                </Stack>
              </Stack>
              <Typography variant="body2" sx={{ mt: 1.5, fontWeight: 600 }}>
                {stub.title}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                {stub.content}
              </Typography>
            </CardContent>
          </Card>
        </Grid>
      )),
    [stubs, t, handleDeleteStub]
  );

  return (
    <>
      <MainCard
        title={t('shareGroups.page.title')}
        secondary={
          <Stack direction="row" spacing={1}>
            <Button size="small" startIcon={<RefreshIcon />} onClick={() => loadAll(false)}>
              {t('common.refresh')}
            </Button>
            {tab === 0 && (
              <Button
                size="small"
                variant="contained"
                startIcon={<AddIcon />}
                onClick={() => {
                  setEditingGroup(null);
                  setGroupDialogOpen(true);
                }}
              >
                {t('shareGroups.page.createGroup')}
              </Button>
            )}
            {tab === 1 && (
              <Button
                size="small"
                variant="contained"
                startIcon={<AddIcon />}
                onClick={() => {
                  setEditingStub(null);
                  setStubDialogOpen(true);
                }}
              >
                {t('shareGroups.page.createStub')}
              </Button>
            )}
          </Stack>
        }
      >
        <Tabs value={tab} onChange={(_, value) => setTab(value)} sx={{ mb: 2 }}>
          <Tab icon={<LayersIcon fontSize="small" />} iconPosition="start" label={t('shareGroups.tabs.groups')} />
          <Tab icon={<NoteIcon fontSize="small" />} iconPosition="start" label={t('shareGroups.tabs.stubs')} />
          <Tab icon={<InsightsIcon fontSize="small" />} iconPosition="start" label={t('shareGroups.tabs.telemetry')} />
        </Tabs>

        {pools.length === 0 && tab === 0 && (
          <Alert severity="info" sx={{ mb: 2 }}>
            {t('shareGroups.page.noPools')}
          </Alert>
        )}

        {loading ? (
          <Stack alignItems="center" sx={{ py: 6 }}>
            <CircularProgress />
          </Stack>
        ) : tab === 0 ? (
          groups.length === 0 ? (
            <Alert severity="info">{t('shareGroups.page.emptyGroups')}</Alert>
          ) : (
            <Grid container spacing={2}>
              {groupCards}
            </Grid>
          )
        ) : tab === 1 ? (
          stubs.length === 0 ? (
            <Alert severity="info">{t('shareGroups.page.emptyStubs')}</Alert>
          ) : (
            <Grid container spacing={2}>
              {stubCards}
            </Grid>
          )
        ) : (
          <ShareTelemetryTab pools={pools} showMessage={showMessage} />
        )}
      </MainCard>

      <ShareGroupDialog
        open={groupDialogOpen}
        group={editingGroup}
        pools={pools}
        onClose={() => setGroupDialogOpen(false)}
        onSaved={() => loadAll(false)}
        showMessage={showMessage}
      />
      <ShareManageDialog
        open={Boolean(linksGroup)}
        subscription={
          linksGroup ? { ID: linksGroup.subscription_id, Name: linksGroup.pool_name || linksGroup.name, Type: 'collection' } : null
        }
        group={linksGroup}
        onClose={() => {
          setLinksGroup(null);
          loadAll(false);
        }}
        showMessage={showMessage}
      />
      <ShareGroupStubsDialog
        open={stubsDialogOpen}
        group={stubsGroup}
        stubs={stubs}
        cases={cases}
        onClose={() => setStubsDialogOpen(false)}
        onSaved={() => loadAll(false)}
        showMessage={showMessage}
      />
      <ShareGroupActivityDialog open={activityOpen} group={activityGroup} onClose={() => setActivityOpen(false)} />
      <ShareStubDialog
        open={stubDialogOpen}
        stub={editingStub}
        cases={cases}
        onClose={() => setStubDialogOpen(false)}
        onSaved={() => loadAll(false)}
        showMessage={showMessage}
      />

      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity} variant="filled" onClose={() => setSnackbar((prev) => ({ ...prev, open: false }))}>
          {snackbar.message}
        </Alert>
      </Snackbar>
    </>
  );
}
