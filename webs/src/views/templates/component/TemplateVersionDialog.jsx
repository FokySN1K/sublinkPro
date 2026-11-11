import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogContentText from '@mui/material/DialogContentText';
import DialogActions from '@mui/material/DialogActions';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Paper from '@mui/material/Paper';
import Chip from '@mui/material/Chip';
import Stack from '@mui/material/Stack';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Alert from '@mui/material/Alert';
import CircularProgress from '@mui/material/CircularProgress';
import Checkbox from '@mui/material/Checkbox';

import VisibilityOutlinedIcon from '@mui/icons-material/VisibilityOutlined';
import DownloadIcon from '@mui/icons-material/Download';
import RestoreIcon from '@mui/icons-material/Restore';
import CompareArrowsIcon from '@mui/icons-material/CompareArrows';

import Editor from '@monaco-editor/react';

import Pagination from 'components/Pagination';
import useResolvedColorScheme from 'hooks/useResolvedColorScheme';
import { getTemplateVersions, getTemplateVersion, downloadTemplateVersion, restoreTemplateVersion } from 'api/templates';

import TemplateDiffDialog from './TemplateDiffDialog';

const resolveEditorLanguage = (filename = '') => (filename.toLowerCase().endsWith('.conf') ? 'ini' : 'yaml');

const formatVersionTimestamp = (value) => {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString();
};

const formatVersionSize = (bytes) => {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return '-';
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

const SOURCE_CHIP_COLOR = {
  baseline: 'default',
  add: 'success',
  save: 'primary',
  rename: 'secondary',
  restore: 'warning'
};

export default function TemplateVersionDialog({ open, template, onClose, onRestored, onNotify }) {
  const { t } = useTranslation();
  const { isDark } = useResolvedColorScheme();
  const filename = template?.file || '';

  const [versions, setVersions] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(10);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState([]);
  const [downloading, setDownloading] = useState(null);
  const [preview, setPreview] = useState({ open: false, version: null, content: '', loading: false, error: '' });
  const [restoreTarget, setRestoreTarget] = useState(null);
  const [restoring, setRestoring] = useState(false);
  const [diffState, setDiffState] = useState({ open: false, from: 1, to: 0 });

  const loadVersions = useCallback(async () => {
    if (!filename) {
      return;
    }
    setLoading(true);
    setError('');
    try {
      const response = await getTemplateVersions({ filename, page: page + 1, pageSize: rowsPerPage });
      const data = response.data || {};
      setVersions(data.items || []);
      setTotal(typeof data.total === 'number' ? data.total : 0);
    } catch (err) {
      setError(err.message || t('templates.versions.messages.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [filename, page, rowsPerPage, t]);

  useEffect(() => {
    if (open) {
      loadVersions();
    }
  }, [open, loadVersions]);

  const handleToggleSelect = (version) => {
    setSelected((prev) => {
      if (prev.includes(version)) {
        return prev.filter((item) => item !== version);
      }
      if (prev.length >= 2) {
        return prev;
      }
      return [...prev, version].sort((a, b) => a - b);
    });
  };

  const handleCompare = () => {
    if (selected.length === 0) {
      return;
    }
    const sorted = [...selected].sort((a, b) => a - b);
    setDiffState({ open: true, from: sorted[0], to: sorted.length > 1 ? sorted[1] : 0 });
  };

  const handlePreview = async (version) => {
    setPreview({ open: true, version, content: '', loading: true, error: '' });
    try {
      const response = await getTemplateVersion(filename, version);
      const content = response.data?.content || '';
      setPreview((prev) => (prev.version === version ? { ...prev, content, loading: false } : prev));
    } catch (err) {
      setPreview((prev) =>
        prev.version === version ? { ...prev, loading: false, error: err.message || t('templates.versions.messages.previewFailed') } : prev
      );
    }
  };

  const handleDownload = async (version) => {
    setDownloading(version);
    try {
      const blob = await downloadTemplateVersion({ filename, version });
      const url = URL.createObjectURL(blob);
      const extension = filename.includes('.') ? filename.slice(filename.lastIndexOf('.')) : '';
      const baseName = extension ? filename.slice(0, filename.length - extension.length) : filename;
      const link = document.createElement('a');
      link.href = url;
      link.download = `${baseName}.v${version}${extension}`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      onNotify?.(err.message || t('templates.versions.messages.downloadFailed'), 'error');
    } finally {
      setDownloading(null);
    }
  };

  const handleRestore = async () => {
    if (!restoreTarget) {
      return;
    }
    setRestoring(true);
    try {
      await restoreTemplateVersion({ filename, version: restoreTarget });
      onNotify?.(t('templates.versions.messages.restoreSuccess', { version: restoreTarget }), 'success');
      setRestoreTarget(null);
      setSelected([]);
      await loadVersions();
      onRestored?.();
    } catch (err) {
      onNotify?.(err.message || t('templates.versions.messages.restoreFailed'), 'error');
    } finally {
      setRestoring(false);
    }
  };

  const compareLabel = selected.length > 1 ? t('templates.versions.actions.compare') : t('templates.versions.actions.compareWithCurrent');

  return (
    <>
      <Dialog open={open} onClose={onClose} maxWidth="lg" fullWidth>
        <DialogTitle>
          <Stack spacing={1}>
            <Stack
              direction={{ xs: 'column', sm: 'row' }}
              spacing={1}
              justifyContent="space-between"
              alignItems={{ xs: 'stretch', sm: 'center' }}
            >
              <Box>
                <Typography variant="h4">{t('templates.versions.title')}</Typography>
                <Typography variant="body2" color="textSecondary">
                  {t('templates.versions.subtitle', { name: filename, count: total })}
                </Typography>
              </Box>
              <Button variant="outlined" startIcon={<CompareArrowsIcon />} onClick={handleCompare} disabled={selected.length === 0}>
                {compareLabel}
              </Button>
            </Stack>
          </Stack>
        </DialogTitle>
        <DialogContent dividers>
          {error ? (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          ) : null}

          {loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: 260 }}>
              <CircularProgress />
            </Box>
          ) : versions.length === 0 ? (
            <Alert severity="info">{t('templates.versions.messages.noVersions')}</Alert>
          ) : (
            <>
              <TableContainer component={Paper} variant="outlined">
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell padding="checkbox" />
                      <TableCell>{t('templates.versions.fields.version')}</TableCell>
                      <TableCell>{t('templates.versions.fields.createdAt')}</TableCell>
                      <TableCell>{t('templates.versions.fields.source')}</TableCell>
                      <TableCell>{t('templates.versions.fields.author')}</TableCell>
                      <TableCell>{t('templates.versions.fields.size')}</TableCell>
                      <TableCell>{t('templates.versions.fields.status')}</TableCell>
                      <TableCell align="right">{t('templates.versions.fields.actions')}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {versions.map((version) => {
                      const isSelected = selected.includes(version.version);
                      const source = version.source || 'save';
                      return (
                        <TableRow key={version.version} hover selected={isSelected}>
                          <TableCell padding="checkbox">
                            <Checkbox
                              size="small"
                              checked={isSelected}
                              disabled={!isSelected && selected.length >= 2}
                              onChange={() => handleToggleSelect(version.version)}
                              inputProps={{ 'aria-label': `v${version.version}` }}
                            />
                          </TableCell>
                          <TableCell>
                            <Typography variant="body2" sx={{ fontWeight: 600 }}>
                              v{version.version}
                            </Typography>
                          </TableCell>
                          <TableCell>{formatVersionTimestamp(version.createdAt)}</TableCell>
                          <TableCell>
                            <Chip
                              size="small"
                              variant="outlined"
                              color={SOURCE_CHIP_COLOR[source] || 'default'}
                              label={t(`templates.versions.sources.${source}`, { defaultValue: source })}
                            />
                          </TableCell>
                          <TableCell>{version.author || '-'}</TableCell>
                          <TableCell>{formatVersionSize(version.size)}</TableCell>
                          <TableCell>
                            {version.isCurrent ? (
                              <Chip size="small" color="success" variant="outlined" label={t('templates.versions.status.current')} />
                            ) : (
                              '-'
                            )}
                          </TableCell>
                          <TableCell align="right">
                            <IconButton
                              size="small"
                              title={t('templates.versions.actions.preview')}
                              aria-label={t('templates.versions.actions.preview')}
                              onClick={() => handlePreview(version.version)}
                            >
                              <VisibilityOutlinedIcon fontSize="small" />
                            </IconButton>
                            <IconButton
                              size="small"
                              title={t('templates.versions.actions.download')}
                              aria-label={t('templates.versions.actions.download')}
                              disabled={downloading === version.version}
                              onClick={() => handleDownload(version.version)}
                            >
                              {downloading === version.version ? <CircularProgress size={16} /> : <DownloadIcon fontSize="small" />}
                            </IconButton>
                            <IconButton
                              size="small"
                              color="warning"
                              title={t('templates.versions.actions.restore')}
                              aria-label={t('templates.versions.actions.restore')}
                              onClick={() => setRestoreTarget(version.version)}
                            >
                              <RestoreIcon fontSize="small" />
                            </IconButton>
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </TableContainer>

              <Pagination
                page={page}
                pageSize={rowsPerPage}
                totalItems={total}
                onPageChange={(_, newPage) => {
                  setPage(newPage);
                  setSelected([]);
                }}
                onPageSizeChange={(event) => {
                  setRowsPerPage(parseInt(event.target.value, 10));
                  setPage(0);
                  setSelected([]);
                }}
                pageSizeOptions={[10, 20, 50]}
              />
              <Typography variant="caption" color="textSecondary" sx={{ display: 'block', mt: 1 }}>
                {t('templates.versions.actions.selectHint')}
              </Typography>
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={preview.open}
        onClose={() => setPreview({ open: false, version: null, content: '', loading: false, error: '' })}
        maxWidth="lg"
        fullWidth
      >
        <DialogTitle>{t('templates.versions.preview.title', { version: preview.version })}</DialogTitle>
        <DialogContent dividers>
          {preview.loading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: 320 }}>
              <CircularProgress />
            </Box>
          ) : preview.error ? (
            <Alert severity="error">{preview.error}</Alert>
          ) : (
            <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
              <Editor
                height="60vh"
                language={resolveEditorLanguage(filename)}
                value={preview.content}
                theme={isDark ? 'vs-dark' : 'light'}
                options={{
                  readOnly: true,
                  minimap: { enabled: false },
                  fontSize: 13,
                  wordWrap: 'on',
                  automaticLayout: true,
                  scrollBeyondLastLine: false,
                  lineNumbers: 'on'
                }}
              />
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPreview({ open: false, version: null, content: '', loading: false, error: '' })}>
            {t('common.close')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={restoreTarget !== null}
        onClose={() => {
          if (!restoring) {
            setRestoreTarget(null);
          }
        }}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>{t('templates.versions.restore.title')}</DialogTitle>
        <DialogContent>
          <DialogContentText>{t('templates.versions.restore.confirm', { version: restoreTarget })}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRestoreTarget(null)} disabled={restoring}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="contained"
            color="warning"
            onClick={handleRestore}
            disabled={restoring}
            startIcon={restoring ? <CircularProgress size={16} /> : null}
          >
            {t('templates.versions.actions.restore')}
          </Button>
        </DialogActions>
      </Dialog>

      <TemplateDiffDialog
        open={diffState.open}
        filename={filename}
        from={diffState.from}
        to={diffState.to}
        onClose={() => setDiffState((prev) => ({ ...prev, open: false }))}
      />
    </>
  );
}
