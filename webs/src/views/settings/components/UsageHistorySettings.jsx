import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

// material-ui
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import CardHeader from '@mui/material/CardHeader';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';

// icons
import SaveIcon from '@mui/icons-material/Save';
import InsightsIcon from '@mui/icons-material/Insights';

// project imports
import { getUsageHistoryConfig, updateUsageHistoryConfig } from 'api/usageHistory';

// ==============================|| 用量历史设置 ||============================== //

export default function UsageHistorySettings({ showMessage }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [retentionDays, setRetentionDays] = useState(180);
  const [snapshotCount, setSnapshotCount] = useState(0);
  const [minDays, setMinDays] = useState(1);
  const [maxDays, setMaxDays] = useState(3650);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getUsageHistoryConfig();
      if (res.data) {
        setRetentionDays(res.data.retentionDays ?? 180);
        setSnapshotCount(res.data.snapshotCount ?? 0);
        setMinDays(res.data.minRetentionDays ?? 1);
        setMaxDays(res.data.maxRetentionDays ?? 3650);
      }
    } catch (error) {
      console.error('获取用量历史配置失败:', error);
      showMessage(error.message || t('usageHistory.settings.messages.loadFailed'), 'error');
    } finally {
      setLoading(false);
    }
  }, [showMessage, t]);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const handleSave = async () => {
    const days = Number(retentionDays);
    if (!Number.isFinite(days) || days < minDays || days > maxDays) {
      showMessage(t('usageHistory.settings.messages.invalidRange', { min: minDays, max: maxDays }), 'warning');
      return;
    }

    setSaving(true);
    try {
      const res = await updateUsageHistoryConfig({ retentionDays: days });
      if (res.data) {
        setRetentionDays(res.data.retentionDays ?? days);
        setSnapshotCount(res.data.snapshotCount ?? 0);
      }
      showMessage(t('usageHistory.settings.messages.saveSuccess'));
    } catch (error) {
      console.error('保存用量历史配置失败:', error);
      showMessage(error.message || t('usageHistory.settings.messages.saveFailed'), 'error');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card variant="outlined">
      <CardHeader
        avatar={<InsightsIcon color="primary" />}
        title={t('usageHistory.settings.title')}
        subheader={t('usageHistory.settings.subheader')}
      />
      <CardContent>
        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
            <CircularProgress size={28} />
          </Box>
        ) : (
          <Stack spacing={2}>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} alignItems={{ xs: 'stretch', sm: 'flex-start' }}>
              <TextField
                type="number"
                size="small"
                label={t('usageHistory.settings.retentionDays')}
                value={retentionDays}
                onChange={(event) => setRetentionDays(event.target.value)}
                helperText={t('usageHistory.settings.retentionHint', { min: minDays, max: maxDays })}
                slotProps={{ htmlInput: { min: minDays, max: maxDays, step: 1 } }}
                sx={{ maxWidth: { sm: 220 } }}
              />
              <Box sx={{ pt: { sm: 1 } }}>
                <Chip
                  variant="outlined"
                  color="default"
                  label={t('usageHistory.settings.snapshotCount', { count: snapshotCount })}
                  sx={{ height: 28 }}
                />
              </Box>
            </Stack>

            <Alert severity="info" variant="standard">
              <Typography variant="body2">{t('usageHistory.settings.info')}</Typography>
            </Alert>

            <Stack direction="row" justifyContent="flex-end">
              <Button variant="contained" startIcon={<SaveIcon />} onClick={handleSave} disabled={saving}>
                {saving ? t('common.saving') : t('common.save')}
              </Button>
            </Stack>
          </Stack>
        )}
      </CardContent>
    </Card>
  );
}
