import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import Button from '@mui/material/Button';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Stack from '@mui/material/Stack';
import Typography from '@mui/material/Typography';

import { DiffEditor } from '@monaco-editor/react';

import useResolvedColorScheme from 'hooks/useResolvedColorScheme';
import { getTemplateVersionDiff } from 'api/templates';

const resolveEditorLanguage = (filename = '') => (filename.toLowerCase().endsWith('.conf') ? 'ini' : 'yaml');

const resolveEditorTheme = (isDark) => (isDark ? 'vs-dark' : 'light');

export default function TemplateDiffDialog({ open, filename, from, to, onClose }) {
  const { t } = useTranslation();
  const { isDark } = useResolvedColorScheme();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [payload, setPayload] = useState(null);

  useEffect(() => {
    if (!open || !filename) {
      return undefined;
    }

    let cancelled = false;
    setLoading(true);
    setError('');
    setPayload(null);

    getTemplateVersionDiff({ filename, from, to })
      .then((response) => {
        if (!cancelled) {
          setPayload(response.data || null);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err.message || t('templates.versions.messages.diffFailed'));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, filename, from, to, t]);

  const identical = Boolean(payload?.identical);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xl" fullWidth>
      <DialogTitle>
        <Stack spacing={0.5}>
          <Typography variant="h4">{t('templates.versions.diff.title')}</Typography>
          {payload ? (
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap alignItems="center">
              <Typography variant="body2" color="textSecondary">
                {payload.fromLabel} → {payload.toLabel}
              </Typography>
              {!identical && (
                <>
                  <Chip size="small" color="success" variant="outlined" label={`+${payload.additions}`} />
                  <Chip size="small" color="error" variant="outlined" label={`-${payload.deletions}`} />
                </>
              )}
            </Stack>
          ) : null}
        </Stack>
      </DialogTitle>
      <DialogContent dividers>
        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: 320 }}>
            <CircularProgress />
          </Box>
        ) : error ? (
          <Alert severity="error">{error}</Alert>
        ) : identical ? (
          <Alert severity="info">{t('templates.versions.diff.identical')}</Alert>
        ) : payload ? (
          <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
            <DiffEditor
              height="65vh"
              language={resolveEditorLanguage(filename)}
              original={payload.fromContent || ''}
              modified={payload.toContent || ''}
              theme={resolveEditorTheme(isDark)}
              options={{
                readOnly: true,
                originalEditable: false,
                minimap: { enabled: false },
                fontSize: 13,
                wordWrap: 'on',
                automaticLayout: true,
                scrollBeyondLastLine: false,
                renderOverviewRuler: false
              }}
            />
          </Box>
        ) : null}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}
