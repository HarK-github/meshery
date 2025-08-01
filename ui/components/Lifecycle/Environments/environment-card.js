import React from 'react';
import SyncAltIcon from '@mui/icons-material/SyncAlt';
import { Delete, Edit } from '@mui/icons-material';
import { FlipCard } from '../General';
import { useGetEnvironmentConnectionsQuery } from '../../../rtk-query/environments';
import CAN from '@/utils/can';
import { keys } from '@/utils/permission_constants';
import { Grid2, useTheme } from '@sistent/sistent';

import {
  Name,
  IconButton,
  CardWrapper,
  DateLabel,
  DescriptionLabel,
  EmptyDescription,
  TabCount,
  TabTitle,
  PopupButton,
  AllocationButton,
  BulkSelectCheckbox,
  CardTitle,
} from './styles';

export const formattoLongDate = (date) => {
  return new Date(date).toLocaleDateString('en-US', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  });
};

export const TransferButton = ({ title, count, onAssign, disabled }) => {
  const theme = useTheme();
  return (
    <PopupButton disabled={disabled} onClick={onAssign} s>
      <Grid2>
        <TabCount>{count}</TabCount>
        <TabTitle>{title}</TabTitle>
        <SyncAltIcon
          sx={{
            position: 'absolute',
            top: '10px',
            right: '10px',
            color: theme.palette.background?.neutral?.default,
          }}
        />
      </Grid2>
    </PopupButton>
  );
};

/**
 * Renders a environment card component.
 *
 * @param {Object} props - The component props.
 * @param {Object} props.environmentDetails - The details of the environment.
 * @param {string} props.environmentDetails.name - The name of the environment.
 * @param {string} props.environmentDetails.description - The description of the environment.
 * @param {Function} props.onDelete - Function to delete the environment.
 * @param {Function} props.onEdit - Function to edit the environment.
 * @param {Function} props.onSelect - Function to select environment for bulk actions.
 * @param {Function} props.onAssignConnection - Function to open connection assignment modal open.
 * @param {Array} props.selectedEnvironments - Selected environments list for delete.
 * @param {String} props.classes - Styles property names for classes.
 *
 */

const EnvironmentCard = ({
  environmentDetails,
  selectedEnvironments,
  onDelete,
  onEdit,
  onSelect,
  onAssignConnection,
}) => {
  const { data: environmentConnections } = useGetEnvironmentConnectionsQuery(
    {
      environmentId: environmentDetails.id,
    },
    { skip: !environmentDetails.id },
  );
  const environmentConnectionsCount = environmentConnections?.total_count || 0;

  // this allows to handle both cases when deleted at is:
  // - timestamp or null
  // - object in format {Time: timestamp, Valid: boolean}
  // --
  // TODO:
  // - switch remote provider to have format of deleted_at as timestamp or null
  // - or update serialisation for deleted_at field of Environment to return object in format {Time: timestamp, Valid: boolean}
  const deleted =
    environmentDetails.deleted_at === null
      ? false
      : typeof environmentDetails.deleted_at === 'object' &&
          environmentDetails.deleted_at !== null &&
          'Valid' in environmentDetails.deleted_at
        ? !!environmentDetails.deleted_at.Valid
        : true;

  return (
    <>
      <FlipCard
        disableFlip={
          selectedEnvironments?.filter((id) => id == environmentDetails.id).length === 1
            ? true
            : false
        }
        frontComponents={
          <CardWrapper
            sx={{
              minHeight: '320px',
              height: '320px',
              borderRadius: 2,
            }}
          >
            <Grid2 sx={{ display: 'flex', flexDirection: 'row', pb: 1 }}>
              <Name variant="body2" onClick={(e) => e.stopPropagation()}>
                {environmentDetails?.name}
              </Name>
            </Grid2>
            <Grid2
              sx={{
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'flex-start',
              }}
            >
              <Grid2
                sx={{ display: 'flex', justifyContent: 'flex-start' }}
                size={{
                  xs: 12,
                  sm: 9,
                  md: 12,
                }}
              >
                {environmentDetails.description ? (
                  <DescriptionLabel
                    onClick={(e) => e.stopPropagation()}
                    sx={{
                      marginBottom: { xs: 2, sm: 0 },
                      paddingRight: { sm: 2, lg: 0 },
                      marginTop: '0px',
                    }}
                  >
                    {environmentDetails.description}
                  </DescriptionLabel>
                ) : (
                  <EmptyDescription
                    onClick={(e) => e.stopPropagation()}
                    sx={{ color: 'rgba(122,132,142,1)' }}
                  >
                    No description
                  </EmptyDescription>
                )}
              </Grid2>
              <Grid2
                size={{
                  xs: 12,
                }}
                sx={{
                  paddingTop: '15px',
                  display: 'flex',
                  alignItems: 'flex-end',
                  justifyContent: 'flex-end',
                  gap: '10px',
                }}
              >
                <AllocationButton onClick={(e) => e.stopPropagation()}>
                  <TransferButton
                    title="Assigned Connections"
                    count={environmentConnectionsCount}
                    onAssign={onAssignConnection}
                    disabled={!CAN(keys.VIEW_CONNECTIONS.action, keys.VIEW_CONNECTIONS.subject)}
                  />
                </AllocationButton>
                {/* temporary disable workspace allocation button  */}
                {/* {false && (
                  <AllocationButton onClick={(e) => e.stopPropagation()}>
                    <TransferButton
                      title="Assigned Workspaces"
                      count={
                        environmentDetails.workspaces ? environmentDetails.workspaces?.length : 0
                      }
                      onAssign={onAssignConnection}
                      disabled={!CAN(keys.VIEW_WORKSPACE.action, keys.VIEW_WORKSPACE.subject)}
                    />
                  </AllocationButton>
                )} */}
              </Grid2>
            </Grid2>
          </CardWrapper>
        }
        backComponents={
          <CardWrapper
            elevation={2}
            sx={{
              minHeight: '320px',
              background: 'linear-gradient(180deg, #007366 0%, #000 100%)',
            }}
          >
            <Grid2 sx={{ display: 'flex', flexDirection: 'row' }} size={{ xs: 12 }}>
              <Grid2 sx={{ display: 'flex', alignItems: 'flex-start' }} size={{ xs: 6 }}>
                <BulkSelectCheckbox
                  onClick={(e) => e.stopPropagation()}
                  onChange={onSelect}
                  disabled={deleted ? true : false}
                />
                <CardTitle
                  sx={{ color: 'white' }}
                  variant="body2"
                  onClick={(e) => e.stopPropagation()}
                >
                  {environmentDetails?.name}
                </CardTitle>
              </Grid2>
              <Grid2
                size={{ xs: 6 }}
                sx={{
                  display: 'flex',
                  alignItems: 'flex-start',
                  justifyContent: 'flex-end',
                }}
              >
                <IconButton
                  onClick={onEdit}
                  disabled={
                    selectedEnvironments?.filter((id) => id == environmentDetails.id).length === 1
                      ? true
                      : !CAN(keys.EDIT_ENVIRONMENT.action, keys.EDIT_ENVIRONMENT.subject)
                  }
                >
                  <Edit sx={{ color: 'white', margin: '0 2px' }} />
                </IconButton>
                <IconButton
                  onClick={onDelete}
                  disabled={
                    selectedEnvironments?.filter((id) => id == environmentDetails.id).length === 1
                      ? true
                      : !CAN(keys.DELETE_ENVIRONMENT.action, keys.DELETE_ENVIRONMENT.subject)
                  }
                >
                  <Delete sx={{ color: 'white', margin: '0 2px' }} />
                </IconButton>
              </Grid2>
            </Grid2>
            <Grid2 sx={{ display: 'flex', flexDirection: 'row', color: 'white' }}>
              <Grid2 size={{ xs: 6 }} sx={{ textAlign: 'left' }}>
                <DateLabel variant="span" onClick={(e) => e.stopPropagation()}>
                  Updated At: {formattoLongDate(environmentDetails?.updated_at)}
                </DateLabel>
              </Grid2>
              <Grid2 size={{ xs: 6 }} sx={{ textAlign: 'left' }}>
                <DateLabel variant="span" onClick={(e) => e.stopPropagation()}>
                  Created At: {formattoLongDate(environmentDetails?.created_at)}
                </DateLabel>
              </Grid2>
            </Grid2>
          </CardWrapper>
        }
      />
    </>
  );
};

export default EnvironmentCard;
