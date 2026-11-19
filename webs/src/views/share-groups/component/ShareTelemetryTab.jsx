import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import MenuItem from '@mui/material/MenuItem';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import RefreshIcon from '@mui/icons-material/Refresh';

import ShareDevicesPanel from './ShareDevicesPanel';
import ShareSubscriptionsPanel from './ShareSubscriptionsPanel';
import ShareUsersPanel from './ShareUsersPanel';

const DAY_OPTIONS = [7, 30];

// Telemetry tab with three scopes: subscription cards (default), individual
// installations and owning users. The pool and period filters apply to every
// scope.
export default function ShareTelemetryTab({ pools = [], showMessage }) {
  const { t } = useTranslation();

  const [mode, setMode] = useState('subscriptions');
  const [subId, setSubId] = useState('');
  const [days, setDays] = useState(7);
  const [refreshKey, setRefreshKey] = useState(0);

  const handleModeChange = useCallback((_, value) => {
    if (value) setMode(value);
  }, []);

  const handlePoolChange = useCallback((event) => {
    setSubId(event.target.value);
  }, []);

  const handleDaysChange = useCallback((event) => {
    setDays(Number(event.target.value) || 7);
  }, []);

  const handleRefresh = useCallback(() => {
    setRefreshKey((prev) => prev + 1);
  }, []);

  const subtitleKey =
    mode === 'subscriptions'
      ? 'shareGroups.subscriptions.subtitle'
      : mode === 'devices'
        ? 'shareGroups.telemetry.subtitle'
        : 'shareGroups.users.subtitle';

  return (
    <Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t(subtitleKey)}
      </Typography>

      <Stack direction={{ xs: 'column', md: 'row' }} spacing={1} alignItems={{ md: 'center' }} sx={{ mb: 2 }}>
        <ToggleButtonGroup size="small" exclusive value={mode} onChange={handleModeChange}>
          <ToggleButton value="subscriptions">{t('shareGroups.telemetry.modes.subscriptions')}</ToggleButton>
          <ToggleButton value="devices">{t('shareGroups.telemetry.modes.devices')}</ToggleButton>
          <ToggleButton value="users">{t('shareGroups.telemetry.modes.users')}</ToggleButton>
        </ToggleButtonGroup>
        <TextField
          select
          size="small"
          label={t('shareGroups.telemetry.filters.pool')}
          value={subId}
          onChange={handlePoolChange}
          sx={{ minWidth: { md: 200 } }}
        >
          <MenuItem value="">{t('shareGroups.telemetry.filters.poolAll')}</MenuItem>
          {pools.map((pool) => (
            <MenuItem key={pool.ID} value={pool.ID}>
              {pool.Name || `#${pool.ID}`}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label={t('shareGroups.telemetry.filters.days')}
          value={days}
          onChange={handleDaysChange}
          sx={{ minWidth: { md: 160 } }}
        >
          {DAY_OPTIONS.map((value) => (
            <MenuItem key={value} value={value}>
              {t('shareGroups.telemetry.filters.daysValue', { days: value })}
            </MenuItem>
          ))}
        </TextField>
        <Box sx={{ flexGrow: 1 }} />
        <Tooltip title={t('common.refresh')}>
          <IconButton size="small" onClick={handleRefresh}>
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>

      {mode === 'users' ? (
        <ShareUsersPanel subId={subId} days={days} showMessage={showMessage} refreshKey={refreshKey} />
      ) : mode === 'subscriptions' ? (
        <ShareSubscriptionsPanel subId={subId} days={days} showMessage={showMessage} refreshKey={refreshKey} />
      ) : (
        <ShareDevicesPanel subId={subId} days={days} showMessage={showMessage} refreshKey={refreshKey} />
      )}
    </Box>
  );
}
