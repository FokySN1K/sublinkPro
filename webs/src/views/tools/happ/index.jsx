import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

// material-ui
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import IconButton from '@mui/material/IconButton';
import Snackbar from '@mui/material/Snackbar';
import Stack from '@mui/material/Stack';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';

// icons
import ClearRoundedIcon from '@mui/icons-material/ClearRounded';
import ContentCopyRoundedIcon from '@mui/icons-material/ContentCopyRounded';
import LockOpenRoundedIcon from '@mui/icons-material/LockOpenRounded';
import OpenInNewRoundedIcon from '@mui/icons-material/OpenInNewRounded';

// project imports
import MainCard from 'ui-component/cards/MainCard';
import { decryptHappLink } from 'api/happ';

// ==============================|| HAPP DECRYPTOR ||============================== //

export default function HappDecryptor() {
  const { t } = useTranslation();
  const [link, setLink] = useState('');
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [snackbar, setSnackbar] = useState({ open: false, message: '', severity: 'success' });

  const showMessage = useCallback((message, severity = 'success') => {
    setSnackbar({ open: true, message, severity });
  }, []);

  const handleSnackbarClose = useCallback(() => {
    setSnackbar((prev) => ({ ...prev, open: false }));
  }, []);

  const handleInputChange = useCallback((event) => {
    setLink(event.target.value);
    setError('');
    setResult(null);
  }, []);

  const handleClear = useCallback(() => {
    setLink('');
    setError('');
    setResult(null);
  }, []);

  const handleDecrypt = useCallback(async () => {
    const trimmed = link.trim();
    setError('');
    setResult(null);
    if (!trimmed) {
      setError(t('happDecryptor.linkRequired'));
      return;
    }
    if (!trimmed.toLowerCase().startsWith('happ://')) {
      setError(t('happDecryptor.invalidLink'));
      return;
    }
    setLoading(true);
    try {
      const response = await decryptHappLink(trimmed);
      const decrypted = response?.data?.url;
      if (!decrypted) {
        setError(t('auth.fallback.operationFailed'));
        return;
      }
      setResult({ url: decrypted, generation: response?.data?.generation || '' });
    } catch (err) {
      setError(err?.message || t('auth.fallback.operationFailed'));
    } finally {
      setLoading(false);
    }
  }, [link, t]);

  const handleCopy = useCallback(async () => {
    if (!result?.url) return;
    try {
      await navigator.clipboard.writeText(result.url);
      showMessage(t('happDecryptor.copied'));
    } catch {
      showMessage(t('happDecryptor.copyFailed'), 'error');
    }
  }, [result, showMessage, t]);

  const canOpen = useMemo(() => /^https?:\/\//i.test(result?.url || ''), [result]);

  return (
    <>
      <MainCard title={t('navigation.items.happDecryptor')}>
        <Stack spacing={2.5}>
          <Typography variant="body2" color="text.secondary">
            {t('happDecryptor.subtitle')}
          </Typography>

          <Alert severity="info" variant="outlined">
            {t('happDecryptor.privacy')}
          </Alert>

          <TextField
            fullWidth
            multiline
            minRows={4}
            maxRows={10}
            label={t('happDecryptor.inputLabel')}
            placeholder={t('happDecryptor.inputPlaceholder')}
            value={link}
            onChange={handleInputChange}
            disabled={loading}
            slotProps={{ input: { sx: { fontFamily: 'monospace', fontSize: '0.85rem' } } }}
          />

          <Stack direction="row" spacing={1.5} flexWrap="wrap" useFlexGap>
            <Button variant="contained" onClick={handleDecrypt} disabled={loading || !link.trim()} startIcon={<LockOpenRoundedIcon />}>
              {loading ? t('happDecryptor.decrypting') : t('happDecryptor.decrypt')}
            </Button>
            <Button
              variant="outlined"
              color="secondary"
              onClick={handleClear}
              disabled={loading || (!link && !result)}
              startIcon={<ClearRoundedIcon />}
            >
              {t('happDecryptor.clear')}
            </Button>
          </Stack>

          {error && (
            <Alert severity="error" onClose={() => setError('')}>
              {error}
            </Alert>
          )}

          {result && (
            <Box
              sx={{
                p: 2,
                borderRadius: 2,
                border: 1,
                borderColor: 'divider',
                bgcolor: 'background.default'
              }}
            >
              <Stack direction="row" spacing={1} alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
                <Typography variant="subtitle2">{t('happDecryptor.resultTitle')}</Typography>
                {result.generation && (
                  <Chip
                    size="small"
                    color="primary"
                    variant="outlined"
                    label={t('happDecryptor.generation', { type: result.generation })}
                  />
                )}
              </Stack>
              <Stack direction="row" spacing={1} alignItems="flex-start">
                <Typography variant="body2" sx={{ flex: 1, minWidth: 0, fontFamily: 'monospace', wordBreak: 'break-all' }}>
                  {result.url}
                </Typography>
                <Tooltip title={t('happDecryptor.copy')}>
                  <IconButton size="small" onClick={handleCopy} aria-label={t('happDecryptor.copy')}>
                    <ContentCopyRoundedIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                {canOpen && (
                  <Tooltip title={t('happDecryptor.open')}>
                    <IconButton
                      size="small"
                      component="a"
                      href={result.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      aria-label={t('happDecryptor.open')}
                    >
                      <OpenInNewRoundedIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                )}
              </Stack>
            </Box>
          )}

          <Alert severity="success" variant="outlined">
            {t('happDecryptor.airportHint')}
          </Alert>
        </Stack>
      </MainCard>

      {/* 提示消息 */}
      <Snackbar
        open={snackbar.open}
        autoHideDuration={3000}
        onClose={handleSnackbarClose}
        anchorOrigin={{ vertical: 'top', horizontal: 'center' }}
      >
        <Alert severity={snackbar.severity}>{snackbar.message}</Alert>
      </Snackbar>
    </>
  );
}
