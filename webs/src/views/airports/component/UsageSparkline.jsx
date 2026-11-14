import PropTypes from 'prop-types';
import { useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import { useTheme } from '@mui/material/styles';
import { withAlpha } from 'utils/colorUtils';
import { formatBytes } from '../utils';

const VIEW_WIDTH = 100;
const VIEW_HEIGHT = 32;
const VERTICAL_PADDING = 4;

// 悬停时间标签：MM-DD HH:mm
const formatSampleTime = (timestamp) => {
  const date = new Date(Number(timestamp));
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value) => String(value).padStart(2, '0');
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
};

/**
 * 将用量历史点转换为 SVG 坐标。
 * 返回 null 表示没有可用于绘制的数据点。
 */
function buildSparklineGeometry(points) {
  const validPoints = (points || []).filter((point) => Number.isFinite(Number(point?.timestamp)));
  if (validPoints.length === 0) return null;

  const values = validPoints.map((point) => Number(point.used) || 0);
  const times = validPoints.map((point) => Number(point.timestamp) || 0);

  const minTime = Math.min(...times);
  const maxTime = Math.max(...times);
  const minValue = Math.min(...values);
  const maxValue = Math.max(...values);

  const timeSpan = maxTime - minTime;
  const valueSpan = maxValue - minValue;
  const drawableHeight = VIEW_HEIGHT - VERTICAL_PADDING * 2;
  // 只有一个采样点时折线画不出来，居中显示为圆点
  const isSinglePoint = validPoints.length === 1;

  const coordinates = validPoints.map((point, index) => {
    const timeRatio = isSinglePoint
      ? 0.5
      : timeSpan > 0
        ? (times[index] - minTime) / timeSpan
        : index / Math.max(validPoints.length - 1, 1);
    const valueRatio = valueSpan > 0 ? (values[index] - minValue) / valueSpan : 0.5;

    return {
      x: timeRatio * VIEW_WIDTH,
      y: VIEW_HEIGHT - VERTICAL_PADDING - valueRatio * drawableHeight,
      failed: Boolean(point.failed),
      timestamp: times[index],
      used: values[index],
      upload: Number(point.upload) || 0,
      download: Number(point.download) || 0
    };
  });

  const linePath = isSinglePoint
    ? ''
    : coordinates.map((coordinate, index) => `${index === 0 ? 'M' : 'L'}${coordinate.x.toFixed(2)},${coordinate.y.toFixed(2)}`).join(' ');
  const areaPath = isSinglePoint ? '' : `${linePath} L${VIEW_WIDTH},${VIEW_HEIGHT} L0,${VIEW_HEIGHT} Z`;

  return {
    isSinglePoint,
    linePath,
    areaPath,
    // 悬停时按坐标查找最近的采样点
    samples: coordinates,
    // 失败点用竖直红线标记，避免在非等比缩放下变形
    failedMarkers: coordinates.filter((coordinate) => coordinate.failed).map((coordinate) => coordinate.x)
  };
}

/**
 * 机场卡片内的用量迷你走势图。
 * 仅绘制累计用量曲线，失败时间点以红色竖线标记。
 */
function UsageSparkline({ points, color, height = 34, onClick, ariaLabel, emptyLabel }) {
  const theme = useTheme();
  const { t } = useTranslation();
  const gradientId = `usage-sparkline-${useId().replace(/:/g, '')}`;
  const geometry = useMemo(() => buildSparklineGeometry(points), [points]);
  const [hoverIndex, setHoverIndex] = useState(-1);
  const strokeColor = color || theme.palette.primary.main;
  const failedColor = theme.palette.error.main;
  const interactive = typeof onClick === 'function';
  const hoverSample = hoverIndex >= 0 ? geometry?.samples?.[hoverIndex] || null : null;

  // 指针横向位置映射到时间轴，取最接近的采样点
  const handleMouseMove = (event) => {
    if (!geometry) return;
    const rect = event.currentTarget.getBoundingClientRect();
    if (!rect.width) return;
    const ratio = Math.min(Math.max((event.clientX - rect.left) / rect.width, 0), 1);
    const targetX = ratio * VIEW_WIDTH;
    let nearestIndex = 0;
    let nearestDistance = Number.POSITIVE_INFINITY;
    geometry.samples.forEach((sample, index) => {
      const distance = Math.abs(sample.x - targetX);
      if (distance < nearestDistance) {
        nearestIndex = index;
        nearestDistance = distance;
      }
    });
    setHoverIndex(nearestIndex);
  };

  const handleMouseLeave = () => setHoverIndex(-1);

  return (
    <Box
      onClick={interactive ? onClick : undefined}
      onMouseMove={geometry ? handleMouseMove : undefined}
      onMouseLeave={geometry ? handleMouseLeave : undefined}
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? ariaLabel : undefined}
      onKeyDown={
        interactive
          ? (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                onClick(event);
              }
            }
          : undefined
      }
      sx={{
        position: 'relative',
        width: '100%',
        height,
        borderRadius: 1,
        cursor: interactive ? 'pointer' : 'default',
        '&:focus-visible': interactive ? { outline: `2px solid ${theme.palette.primary.main}`, outlineOffset: 1 } : undefined
      }}
    >
      {geometry ? (
        <svg
          viewBox={`0 0 ${VIEW_WIDTH} ${VIEW_HEIGHT}`}
          width="100%"
          height="100%"
          preserveAspectRatio="none"
          aria-hidden="true"
          style={{ display: 'block', overflow: 'visible' }}
        >
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={withAlpha(strokeColor, 0.32)} />
              <stop offset="100%" stopColor={withAlpha(strokeColor, 0)} />
            </linearGradient>
          </defs>
          {!geometry.isSinglePoint && (
            <>
              <path d={geometry.areaPath} fill={`url(#${gradientId})`} stroke="none" />
              <path
                d={geometry.linePath}
                fill="none"
                stroke={strokeColor}
                strokeWidth={1.6}
                strokeLinecap="round"
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
              />
            </>
          )}
          {geometry.failedMarkers.map((markerX, index) => (
            <rect key={`failed-${index}`} x={markerX - 0.4} y={0} width={0.8} height={VIEW_HEIGHT} fill={failedColor} opacity={0.55} />
          ))}
        </svg>
      ) : (
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            height: '100%',
            borderTop: `1px dashed ${withAlpha(theme.palette.text.secondary, 0.32)}`,
            borderBottom: `1px dashed ${withAlpha(theme.palette.text.secondary, 0.32)}`,
            color: 'text.secondary'
          }}
        >
          <Box component="span" sx={{ fontSize: '0.62rem', lineHeight: 1 }}>
            {emptyLabel}
          </Box>
        </Box>
      )}
      {hoverSample && (
        <>
          <Box
            aria-hidden="true"
            sx={{
              position: 'absolute',
              top: 0,
              bottom: 0,
              left: `${hoverSample.x}%`,
              width: '1px',
              bgcolor: withAlpha(strokeColor, 0.4),
              pointerEvents: 'none'
            }}
          />
          <Box
            aria-hidden="true"
            sx={{
              position: 'absolute',
              left: `${hoverSample.x}%`,
              top: `${(hoverSample.y / VIEW_HEIGHT) * 100}%`,
              width: 7,
              height: 7,
              borderRadius: '50%',
              bgcolor: hoverSample.failed ? failedColor : strokeColor,
              border: '1px solid',
              borderColor: 'background.paper',
              boxShadow: `0 0 0 3px ${withAlpha(hoverSample.failed ? failedColor : strokeColor, 0.2)}`,
              transform: 'translate(-50%, -50%)',
              pointerEvents: 'none',
              zIndex: 2
            }}
          />
          <Box
            aria-hidden="true"
            sx={{
              position: 'absolute',
              left: `${Math.min(Math.max(hoverSample.x, 10), 90)}%`,
              top: -4,
              transform: 'translate(-50%, -100%)',
              px: 0.75,
              py: 0.5,
              minWidth: 108,
              borderRadius: 1,
              bgcolor: 'background.paper',
              border: '1px solid',
              borderColor: 'divider',
              boxShadow: '0 8px 20px rgba(0, 0, 0, 0.18)',
              color: 'text.primary',
              fontSize: '0.65rem',
              lineHeight: 1.45,
              whiteSpace: 'nowrap',
              pointerEvents: 'none',
              zIndex: 3
            }}
          >
            <Box component="span" sx={{ display: 'block', fontWeight: 600 }}>
              {formatSampleTime(hoverSample.timestamp)}
            </Box>
            <Box component="span" sx={{ display: 'block' }}>
              {`${t('usageHistory.tooltip.used')} ${formatBytes(hoverSample.used)}`}
            </Box>
            <Box component="span" sx={{ display: 'block', color: 'text.secondary' }}>
              {`↑ ${formatBytes(hoverSample.upload)} · ↓ ${formatBytes(hoverSample.download)}`}
            </Box>
            {hoverSample.failed && (
              <Box component="span" sx={{ display: 'block', color: 'error.main' }}>
                {t('usageHistory.tooltip.failed')}
              </Box>
            )}
          </Box>
        </>
      )}
      {geometry?.isSinglePoint && (
        <Box
          sx={{
            position: 'absolute',
            left: '50%',
            top: '50%',
            width: 7,
            height: 7,
            borderRadius: '50%',
            bgcolor: strokeColor,
            transform: 'translate(-50%, -50%)',
            boxShadow: `0 0 0 3px ${withAlpha(strokeColor, 0.18)}`
          }}
        />
      )}
    </Box>
  );
}

UsageSparkline.propTypes = {
  points: PropTypes.arrayOf(
    PropTypes.shape({
      timestamp: PropTypes.number,
      used: PropTypes.number,
      upload: PropTypes.number,
      download: PropTypes.number,
      failed: PropTypes.bool
    })
  ),
  color: PropTypes.string,
  height: PropTypes.number,
  onClick: PropTypes.func,
  ariaLabel: PropTypes.string,
  emptyLabel: PropTypes.string
};

export default UsageSparkline;
